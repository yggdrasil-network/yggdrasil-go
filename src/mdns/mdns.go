package mdns

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"time"

	"github.com/Arceliar/phony"
	"github.com/brutella/dnssd"
	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

type MDNS struct {
	phony.Inbox
	core      *core.Core
	config    *config.NodeState
	context   context.Context    // global context
	cancel    context.CancelFunc // cancels all interfaces
	log       *log.Logger
	info      map[string]string
	responder dnssd.Responder
	_running  bool
	_exprs    []*regexp.Regexp
	_servers  map[string]map[string]*mDNSInterface // intf -> ip -> *mDNSServer
}

type mDNSInterface struct {
	context  context.Context    // parent context is in the MDNS struct
	cancel   context.CancelFunc // cancels this interface only
	mdns     *MDNS
	addr     *net.TCPAddr
	intf     net.Interface
	service  dnssd.ServiceHandle
	listener *core.TcpListener
}

var protoVersion = fmt.Sprintf("%d.%d", core.ProtocolMajorVersion, core.ProtocolMinorVersion)

func (m *MDNS) Init(c *core.Core, state *config.NodeState, log *log.Logger, options interface{}) error {
	pk := c.PrivateKey().Public().(ed25519.PublicKey)
	m.context, m.cancel = context.WithCancel(context.Background())
	m.core = c
	m.config = state
	m.log = log
	m.info = map[string]string{
		"ed25519": hex.EncodeToString(pk),
		"proto":   protoVersion,
	}

	// Now get a list of interface expressions from the
	// config. This will dictate which interfaces we are
	// allowed to use.
	var exprs []*regexp.Regexp
	// Compile each regular expression
	for _, exstr := range m.config.Current.MulticastInterfaces {
		e, err := regexp.Compile(exstr)
		if err != nil {
			return err
		}
		exprs = append(exprs, e)
	}
	// Update our expression list.
	m._exprs = exprs

	return nil
}

func (m *MDNS) Start() error {
	var err error
	phony.Block(m, func() {
		if err = m._start(); err == nil {
			m._running = true
		}
	})
	m.log.Infoln("Started mDNS module")
	return err
}

func (m *MDNS) _start() error {
	if m._running {
		return errors.New("mDNS module is already running")
	}

	m._servers = make(map[string]map[string]*mDNSInterface)

	var err error
	m.responder, err = dnssd.NewResponder()
	if err != nil {
		return fmt.Errorf("dnssd.NewResponder: %w", err)
	}
	go m.responder.Respond(m.context) // nolint:errcheck

	m.Act(m, m._updateInterfaces)

	return nil
}

func (m *MDNS) Stop() error {
	var err error
	phony.Block(m, func() {
		err = m._stop()
	})
	m.log.Infoln("Stopped mDNS module")
	return err
}

func (m *MDNS) _stop() error {
	for _, intf := range m._servers {
		for _, ip := range intf {
			m.responder.Remove(ip.service)
			ip.listener.Stop()
		}
	}
	return nil
}

func (m *MDNS) SetupAdminHandlers(a *admin.AdminSocket) {}

func (m *MDNS) IsStarted() bool {
	var running bool
	phony.Block(m, func() {
		running = m._running
	})
	return running
}

func (m *MDNS) _updateInterfaces() {
	// Start by getting a list of interfaces from the
	// operating system.
	interfaces := make(map[string]map[string]net.Interface)
	osInterfaces, err := net.Interfaces()
	if err != nil {
		return
	}
	// Now work through the OS interfaces and work out
	// which are suitable.
	for _, intf := range osInterfaces {
		if intf.Flags&net.FlagUp == 0 {
			continue
		}
		if intf.Flags&net.FlagMulticast == 0 {
			continue
		}
		if intf.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		for _, expr := range m._exprs {
			// Does the interface match the regular expression? Store it if so
			if expr.MatchString(intf.Name) {
				// We should now work out if there are any good candidate
				// IP addresses on this interface. We will only store it if
				// there are.
				addrs, err := intf.Addrs()
				if err != nil {
					continue
				}
				for _, addr := range addrs {
					ip, _, err := net.ParseCIDR(addr.String())
					if err != nil {
						continue
					}
					if ip.IsLinkLocalUnicast() {
						if _, ok := interfaces[intf.Name]; !ok {
							interfaces[intf.Name] = make(map[string]net.Interface)
						}
						interfaces[intf.Name][ip.String()] = intf
					}
				}
			}
		}
	}

	// Work out which interfaces are new.
	for n, addrs := range interfaces {
		if _, ok := m._servers[n]; !ok {
			m._servers[n] = make(map[string]*mDNSInterface)
		}
		for addr, intf := range addrs {
			if _, ok := m._servers[n][addr]; !ok {
				if err := m._startInterface(intf, addr); err != nil {
					m.log.Errorf("Failed to start mDNS interface %s on address %s: %s", n, addr, err)
				} else {
					m.log.Infof("mDNS started on interface %s address %s", n, addr)
				}
			}
		}
	}

	// Work out which interfaces have disappeared.
	for n := range m._servers {
		if addrs, ok := interfaces[n]; !ok {
			for addr, server := range m._servers[n] {
				if err := m._stopInterface(server.intf, addr); err != nil {
					m.log.Errorf("Failed to stop mDNS interface %s on address %s: %s", n, addr, err)
				} else {
					m.log.Infof("mDNS stopped on interface %s address %s", n, addr)
				}
			}
		} else {
			for addr := range addrs {
				if intf, ok := interfaces[n][addr]; !ok {
					if err := m._stopInterface(intf, addr); err != nil {
						m.log.Errorf("Failed to stop mDNS interface %s on address %s: %s", n, addr, err)
					} else {
						m.log.Infof("mDNS stopped on interface %s address %s", n, addr)
					}
				}
			}
		}
	}
}

func (m *MDNS) _startInterface(intf net.Interface, addr string) error {
	// Don't start a new interface if it is already alive.
	if _, ok := m._servers[intf.Name][addr]; ok {
		return errors.New("already started")
	}

	// Construct a listener on this address.
	// Work out what the listen address of the new TCP listener should be.
	ip := net.ParseIP(addr)
	listenaddr := fmt.Sprintf("[%s]:%d", ip.String(), m.config.Current.LinkLocalTCPPort)
	if ip.To4() != nil {
		listenaddr = fmt.Sprintf("%s:%d", ip.String(), m.config.Current.LinkLocalTCPPort)
	}
	listener, err := m.core.Listen(&url.URL{
		Scheme: "tcp",
		Host:   listenaddr,
	}, intf.Name)
	if err != nil {
		return fmt.Errorf("m.core.ListenTCP (%s): %w", listenaddr, err)
	}

	fmt.Println("Listener address is", listener.Listener.Addr().String())

	// Resolve it as a TCP endpoint so that we can get the IP address and
	// port separately.
	tcpaddr, err := net.ResolveTCPAddr(
		listener.Listener.Addr().Network(),
		listener.Listener.Addr().String(),
	)
	if err != nil {
		return fmt.Errorf("net.ResolveTCPAddr: %w", err)
	}

	pk := m.core.PrivateKey().Public().(ed25519.PublicKey)
	svc, err := dnssd.NewService(dnssd.Config{
		Name:   hex.EncodeToString(pk[:8]),
		Type:   "_yggdrasil._tcp",
		Domain: "local",
		Text:   m.info,
		Port:   tcpaddr.Port,
		IPs:    []net.IP{ip},
		Ifaces: []string{intf.Name},
	})
	if err != nil {
		return fmt.Errorf("dnssd.NewService: %w", err)
	}

	// Add the service to the responder.
	handle, err := m.responder.Add(svc)
	if err != nil {
		return fmt.Errorf("m.responder.Add: %w", err)
	}

	// Now store information about our new listener and server.
	if _, ok := m._servers[intf.Name]; !ok {
		m._servers[intf.Name] = make(map[string]*mDNSInterface)
	}
	ctx, cancel := context.WithCancel(m.context)
	m._servers[intf.Name][addr] = &mDNSInterface{
		context:  ctx,
		cancel:   cancel,
		mdns:     m,
		intf:     intf,
		addr:     tcpaddr,
		service:  handle,
		listener: listener,
	}
	go m._servers[intf.Name][addr].listen()

	return nil
}

func (m *MDNS) _stopInterface(intf net.Interface, addr string) error {
	// Check if we know about the interface.
	addrs, aok := m._servers[intf.Name]
	if !aok {
		return fmt.Errorf("interface %q not found", intf.Name)
	}

	// Check if we know about the address on that interface.
	server, sok := addrs[addr]
	if !sok {
		return fmt.Errorf("address %q not found", addr)
	}

	// Shut down the mDNS server and the TCP listener.
	server.cancel()
	m.responder.Remove(server.service)
	server.listener.Stop()

	// Clean up.
	delete(m._servers[intf.Name], addr)
	if len(m._servers[intf.Name]) == 0 {
		delete(m._servers, intf.Name)
	}

	return nil
}

func (s *mDNSInterface) listen() {
	ourpk := hex.EncodeToString(s.mdns.core.PrivateKey().Public().(ed25519.PublicKey))

	add := func(e dnssd.BrowseEntry) {
		if len(e.IPs) == 0 {
			return
		}
		if version := e.Text["proto"]; version != protoVersion {
			return
		}
		if pk := e.Text["ed25519"]; pk == ourpk {
			return
		}
		if e.IfaceName != s.intf.Name {
			return
		}
		service, err := dnssd.LookupInstance(s.context, e.ServiceInstanceName())
		if err != nil {
			return
		}
		for _, ip := range service.IPs {
			u := &url.URL{
				Scheme:   "tcp",
				RawQuery: "ed25519=" + e.Text["ed25519"],
			}
			switch {
			case ip.To4() == nil: // IPv6
				u.Host = fmt.Sprintf("[%s%%%s]:%d", ip.String(), e.IfaceName, service.Port)
			case ip.To16() == nil: // IPv4
				u.Host = fmt.Sprintf("%s%%%s:%d", ip.String(), e.IfaceName, service.Port)
			default:
				continue
			}
			s.mdns.log.Debugln("Calling", u.String())
			if err := s.mdns.core.CallPeer(u, e.IfaceName); err != nil {
				continue
			}
			return
		}
	}

	remove := func(e dnssd.BrowseEntry) {
		// the service disappeared
	}

	for {
		select {
		case <-s.context.Done():
			return
		default:
			ctx, cancel := context.WithTimeout(s.context, time.Second*5)
			_ = dnssd.LookupType(ctx, "_yggdrasil._tcp.local.", add, remove)
			cancel()
		}
	}
}
