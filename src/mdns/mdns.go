package mdns

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"regexp"
	"time"

	"github.com/Arceliar/phony"
	"github.com/gologme/log"
	"github.com/grandcat/zeroconf"

	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

const (
	MDNSService = "_yggdrasil._tcp"
	MDNSDomain  = "yggdrasil.local."
)

type MDNS struct {
	phony.Inbox
	core      *yggdrasil.Core                   //
	config    *config.NodeState                 //
	log       *log.Logger                       //
	info      []string                          //
	instance  string                            //
	_running  bool                              //
	_exprs    []*regexp.Regexp                  //
	_servers  map[string]map[string]*mDNSServer // intf -> ip -> *mDNSServer
	_resolver *zeroconf.Resolver                //
	_context  context.Context                   // used by _resolver
	_cancel   context.CancelFunc                // used by _resolver
}

type mDNSServer struct {
	intf     net.Interface
	server   *zeroconf.Server
	listener *yggdrasil.TcpListener
	time     time.Time
}

func (m *MDNS) Init(core *yggdrasil.Core, state *config.NodeState, log *log.Logger, options interface{}) error {
	m.core = core
	m.config = state
	m.log = log
	m.info = []string{
		fmt.Sprintf("ed25519=%s", core.SigningPublicKey()),
		fmt.Sprintf("curve25519=%s", core.EncryptionPublicKey()),
		fmt.Sprintf("versionmajor=%d", yggdrasil.ProtocolMajorVersion),
		fmt.Sprintf("versionminor=%d", yggdrasil.ProtocolMinorVersion),
	}
	if nodeid := core.NodeID(); nodeid != nil {
		m.instance = hex.EncodeToString((*nodeid)[:])[:16]
	}

	current := m.config.GetCurrent()
	m._updateConfig(&current)

	m._context, m._cancel = context.WithCancel(context.Background())
	m._servers = make(map[string]map[string]*mDNSServer)

	return nil
}

func (m *MDNS) listen(results <-chan *zeroconf.ServiceEntry) {
	m.log.Info("Listening for other Yggdrasil nodes via mDNS")
	for entry := range results {
		log.Info("Received mDNS entry:", entry)
	}
	m.log.Info("No longer listening for other Yggdrasil nodes")
}

func (m *MDNS) Start() error {
	var err error
	phony.Block(m, func() {
		err = m._start()
	})
	m.log.Infoln("Started mDNS module")
	return err
}

func (m *MDNS) _start() error {
	var err error
	if m._running {
		return errors.New("mDNS module is already running")
	}

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		m.log.Fatalln("Failed to initialize resolver:", err.Error())
	}
	m._resolver = resolver
	incoming := make(chan *zeroconf.ServiceEntry)
	go m.listen(incoming)

	err = m._resolver.Browse(m._context, MDNSService, MDNSDomain, incoming)
	if err != nil {
		m.log.Fatalln("Failed to browse:", err.Error())
	}

	m._running = true
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
	m._cancel()
	for _, intf := range m._servers {
		for _, ip := range intf {
			ip.server.Shutdown()
			ip.listener.Stop()
		}
	}
	return nil
}

func (m *MDNS) UpdateConfig(config *config.NodeConfig) {
	var err error
	phony.Block(m, func() {
		err = m._updateConfig(config)
	})
	if err != nil {
		m.log.Warnf("Failed to update mDNS config: %s", err)
	} else {
		m.log.Infof("mDNS configuration updated")
	}
}

func (m *MDNS) _updateConfig(config *config.NodeConfig) error {
	// Now get a list of interface expressions from the
	// config. This will dictate which interfaces we are
	// allowed to use.
	var exprs []*regexp.Regexp
	// Compile each regular expression
	for _, exstr := range config.MulticastInterfaces {
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
			for addr, intf := range addrs {
				if err := m._startInterface(intf, addr); err != nil {
					m.log.Errorf("Failed to start mDNS interface %s on address %s: %s", n, addr, err)
				} else {
					m.log.Infof("mDNS started on interface %s address %s", n, addr)
				}
			}
		}
	}

	// Work out which interfaces have disappeared.
	for n, _ := range m._servers {
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

	time.AfterFunc(time.Second, func() {
		m.Act(m, m._updateInterfaces)
	})
}

func (m *MDNS) _startInterface(intf net.Interface, addr string) error {
	// Construct a listener on this address.
	// Work out what the listen address of the new TCP listener should be.
	ip := net.ParseIP(addr)
	listenaddr := fmt.Sprintf("[%s%%%s]:%d", ip, intf.Name, 0) // TODO: linklocalport
	listener, err := m.core.ListenTCP(listenaddr)
	if err != nil {
		return fmt.Errorf("m.core.ListenTCP: %w", err)
	}

	// Resolve it as a TCP endpoint so that we can get the IP address and
	// port separately.
	tcpaddr, err := net.ResolveTCPAddr(
		listener.Listener.Addr().Network(),
		listener.Listener.Addr().String(),
	)
	if err != nil {
		return fmt.Errorf("net.ResolveTCPAddr: %w", err)
	}

	// Register a proxy service. This allows us to specify the hostname and
	// IP addresses to put into the DNS SRV record.
	server, err := zeroconf.RegisterProxy(
		m.instance,                    // instance name
		MDNSService,                   // service name
		MDNSDomain,                    // service domain
		tcpaddr.Port,                  // TCP listener port
		m.instance,                    // our hostname
		[]string{tcpaddr.IP.String()}, // our IP address
		m.info,                        // TXT record contents
		[]net.Interface{intf},         // interfaces to use
	)
	if err != nil {
		return fmt.Errorf("zeroconf.RegisterProxy: %w", err)
	}

	// Now store information about our new listener and server.
	if _, ok := m._servers[intf.Name]; !ok {
		m._servers[intf.Name] = make(map[string]*mDNSServer)
	}
	m._servers[intf.Name][addr] = &mDNSServer{
		intf:     intf,
		server:   server,
		listener: listener,
	}

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
	server.server.Shutdown()
	server.listener.Stop()

	// Clean up.
	delete(m._servers[intf.Name], addr)
	if len(m._servers[intf.Name]) == 0 {
		delete(m._servers, intf.Name)
	}

	return nil
}
