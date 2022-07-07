package multicast

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"time"

	"github.com/Arceliar/phony"
	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"golang.org/x/net/ipv6"
)

// Multicast represents the multicast advertisement and discovery mechanism used
// by Yggdrasil to find peers on the same subnet. When a beacon is received on a
// configured multicast interface, Yggdrasil will attempt to peer with that node
// automatically.
type Multicast struct {
	phony.Inbox
	core        *core.Core
	config      *config.NodeConfig
	log         *log.Logger
	sock        *ipv6.PacketConn
	groupAddr   string
	listeners   map[string]*listenerInfo
	isOpen      bool
	_interfaces map[string]interfaceInfo
}

type interfaceInfo struct {
	iface  net.Interface
	addrs  []net.Addr
	beacon bool
	listen bool
	port   uint16
}

type listenerInfo struct {
	listener *core.TcpListener
	time     time.Time
	interval time.Duration
	port     uint16
}

// Init prepares the multicast interface for use.
func (m *Multicast) Init(core *core.Core, nc *config.NodeConfig, log *log.Logger, options interface{}) error {
	m.core = core
	m.config = nc
	m.log = log
	m.listeners = make(map[string]*listenerInfo)
	m._interfaces = make(map[string]interfaceInfo)
	m.groupAddr = "[ff02::114]:9001"
	return nil
}

// Start starts the multicast interface. This launches goroutines which will
// listen for multicast beacons from other hosts and will advertise multicast
// beacons out to the network.
func (m *Multicast) Start() error {
	var err error
	phony.Block(m, func() {
		err = m._start()
	})
	m.log.Debugln("Started multicast module")
	return err
}

func (m *Multicast) _start() error {
	if m.isOpen {
		return fmt.Errorf("multicast module is already started")
	}
	m.config.RLock()
	defer m.config.RUnlock()
	if len(m.config.MulticastInterfaces) == 0 {
		return nil
	}
	m.log.Infoln("Starting multicast module")
	addr, err := net.ResolveUDPAddr("udp", m.groupAddr)
	if err != nil {
		return err
	}
	listenString := fmt.Sprintf("[::]:%v", addr.Port)
	lc := net.ListenConfig{
		Control: m.multicastReuse,
	}
	conn, err := lc.ListenPacket(context.Background(), "udp6", listenString)
	if err != nil {
		return err
	}
	m.sock = ipv6.NewPacketConn(conn)
	if err = m.sock.SetControlMessage(ipv6.FlagDst, true); err != nil { // nolint:staticcheck
		// Windows can't set this flag, so we need to handle it in other ways
	}

	m.isOpen = true
	go m.listen()
	m.Act(nil, m._multicastStarted)
	m.Act(nil, m._announce)

	return nil
}

// IsStarted returns true if the module has been started.
func (m *Multicast) IsStarted() bool {
	var isOpen bool
	phony.Block(m, func() {
		isOpen = m.isOpen
	})
	return isOpen
}

// Stop stops the multicast module.
func (m *Multicast) Stop() error {
	var err error
	phony.Block(m, func() {
		err = m._stop()
	})
	m.log.Debugln("Stopped multicast module")
	return err
}

func (m *Multicast) _stop() error {
	m.log.Infoln("Stopping multicast module")
	m.isOpen = false
	if m.sock != nil {
		m.sock.Close()
	}
	return nil
}

func (m *Multicast) _updateInterfaces() {
	interfaces := m.getAllowedInterfaces()
	for name, info := range interfaces {
		addrs, err := info.iface.Addrs()
		if err != nil {
			m.log.Warnf("Failed up get addresses for interface %s: %s", name, err)
			delete(interfaces, name)
			continue
		}
		info.addrs = addrs
		interfaces[name] = info
	}
	m._interfaces = interfaces
}

func (m *Multicast) Interfaces() map[string]net.Interface {
	interfaces := make(map[string]net.Interface)
	phony.Block(m, func() {
		for _, info := range m._interfaces {
			interfaces[info.iface.Name] = info.iface
		}
	})
	return interfaces
}

// getAllowedInterfaces returns the currently known/enabled multicast interfaces.
func (m *Multicast) getAllowedInterfaces() map[string]interfaceInfo {
	interfaces := make(map[string]interfaceInfo)
	// Get interface expressions from config
	ifcfgs := m.config.MulticastInterfaces
	// Ask the system for network interfaces
	allifaces, err := net.Interfaces()
	if err != nil {
		// Don't panic, since this may be from e.g. too many open files (from too much connection spam)
		// TODO? log something
		return nil
	}
	// Work out which interfaces to announce on
	for _, iface := range allifaces {
		if iface.Flags&net.FlagUp == 0 {
			// Ignore interfaces that are down
			continue
		}
		if iface.Flags&net.FlagMulticast == 0 {
			// Ignore non-multicast interfaces
			continue
		}
		if iface.Flags&net.FlagPointToPoint != 0 {
			// Ignore point-to-point interfaces
			continue
		}
		for _, ifcfg := range ifcfgs {
			// Compile each regular expression
			e, err := regexp.Compile(ifcfg.Regex)
			if err != nil {
				panic(err)
			}
			// Does the interface match the regular expression? Store it if so
			if e.MatchString(iface.Name) {
				if ifcfg.Beacon || ifcfg.Listen {
					info := interfaceInfo{
						iface:  iface,
						beacon: ifcfg.Beacon,
						listen: ifcfg.Listen,
						port:   ifcfg.Port,
					}
					interfaces[iface.Name] = info
				}
				break
			}
		}
	}
	return interfaces
}

func (m *Multicast) _announce() {
	if !m.isOpen {
		return
	}
	m._updateInterfaces()
	groupAddr, err := net.ResolveUDPAddr("udp6", m.groupAddr)
	if err != nil {
		panic(err)
	}
	destAddr, err := net.ResolveUDPAddr("udp6", m.groupAddr)
	if err != nil {
		panic(err)
	}
	// There might be interfaces that we configured listeners for but are no
	// longer up - if that's the case then we should stop the listeners
	for name, info := range m.listeners {
		// Prepare our stop function!
		stop := func() {
			info.listener.Stop()
			delete(m.listeners, name)
			m.log.Debugln("No longer multicasting on", name)
		}
		// If the interface is no longer visible on the system then stop the
		// listener, as another one will be started further down
		if _, ok := m._interfaces[name]; !ok {
			stop()
			continue
		}
		// It's possible that the link-local listener address has changed so if
		// that is the case then we should clean up the interface listener
		found := false
		listenaddr, err := net.ResolveTCPAddr("tcp6", info.listener.Listener.Addr().String())
		if err != nil {
			stop()
			continue
		}
		// Find the interface that matches the listener
		if info, ok := m._interfaces[name]; ok {
			for _, addr := range info.addrs {
				if ip, _, err := net.ParseCIDR(addr.String()); err == nil {
					// Does the interface address match our listener address?
					if ip.Equal(listenaddr.IP) {
						found = true
						break
					}
				}
			}
		}
		// If the address has not been found on the adapter then we should stop
		// and clean up the TCP listener. A new one will be created below if a
		// suitable link-local address is found
		if !found {
			stop()
		}
	}
	// Now that we have a list of valid interfaces from the operating system,
	// we can start checking if we can send multicasts on them
	for _, info := range m._interfaces {
		iface := info.iface
		for _, addr := range info.addrs {
			addrIP, _, _ := net.ParseCIDR(addr.String())
			// Ignore IPv4 addresses
			if addrIP.To4() != nil {
				continue
			}
			// Ignore non-link-local addresses
			if !addrIP.IsLinkLocalUnicast() {
				continue
			}
			if info.listen {
				// Join the multicast group, so we can listen for beacons
				_ = m.sock.JoinGroup(&iface, groupAddr)
			}
			if !info.beacon {
				break // Don't send multicast beacons or accept incoming connections
			}
			// Try and see if we already have a TCP listener for this interface
			var linfo *listenerInfo
			if nfo, ok := m.listeners[iface.Name]; !ok || nfo.listener.Listener == nil {
				// No listener was found - let's create one
				urlString := fmt.Sprintf("tls://[%s]:%d", addrIP, info.port)
				u, err := url.Parse(urlString)
				if err != nil {
					panic(err)
				}
				if li, err := m.core.Listen(u, iface.Name); err == nil {
					m.log.Debugln("Started multicasting on", iface.Name)
					// Store the listener so that we can stop it later if needed
					linfo = &listenerInfo{listener: li, time: time.Now(), port: info.port}
					m.listeners[iface.Name] = linfo
				} else {
					m.log.Warnln("Not multicasting on", iface.Name, "due to error:", err)
				}
			} else {
				// An existing listener was found
				linfo = m.listeners[iface.Name]
			}
			// Make sure nothing above failed for some reason
			if linfo == nil {
				continue
			}
			if time.Since(linfo.time) < linfo.interval {
				continue
			}
			// Get the listener details and construct the multicast beacon
			lladdr := linfo.listener.Listener.Addr().String()
			if a, err := net.ResolveTCPAddr("tcp6", lladdr); err == nil {
				a.Zone = ""
				destAddr.Zone = iface.Name
				msg := append([]byte(nil), m.core.GetSelf().Key...)
				msg = append(msg, a.IP...)
				pbs := make([]byte, 2)
				binary.BigEndian.PutUint16(pbs, uint16(a.Port))
				msg = append(msg, pbs...)
				_, _ = m.sock.WriteTo(msg, nil, destAddr)
			}
			if linfo.interval.Seconds() < 15 {
				linfo.interval += time.Second
			}
			break
		}
	}
	time.AfterFunc(time.Second, func() {
		m.Act(nil, m._announce)
	})
}

func (m *Multicast) listen() {
	groupAddr, err := net.ResolveUDPAddr("udp6", m.groupAddr)
	if err != nil {
		panic(err)
	}
	bs := make([]byte, 2048)
	for {
		nBytes, rcm, fromAddr, err := m.sock.ReadFrom(bs)
		if err != nil {
			if !m.IsStarted() {
				return
			}
			panic(err)
		}
		if rcm != nil {
			// Windows can't set the flag needed to return a non-nil value here
			// So only make these checks if we get something useful back
			// TODO? Skip them always, I'm not sure if they're really needed...
			if !rcm.Dst.IsLinkLocalMulticast() {
				continue
			}
			if !rcm.Dst.Equal(groupAddr.IP) {
				continue
			}
		}
		if nBytes < ed25519.PublicKeySize {
			continue
		}
		var key ed25519.PublicKey
		key = append(key, bs[:ed25519.PublicKeySize]...)
		if bytes.Equal(key, m.core.GetSelf().Key) {
			continue // don't bother trying to peer with self
		}
		begin := ed25519.PublicKeySize
		end := nBytes - 2
		if end <= begin {
			continue // malformed address
		}
		ip := bs[begin:end]
		port := binary.BigEndian.Uint16(bs[end:nBytes])
		anAddr := net.TCPAddr{IP: ip, Port: int(port)}
		addr, err := net.ResolveTCPAddr("tcp6", anAddr.String())
		if err != nil {
			continue
		}
		from := fromAddr.(*net.UDPAddr)
		if !from.IP.Equal(addr.IP) {
			continue
		}
		var interfaces map[string]interfaceInfo
		phony.Block(m, func() {
			interfaces = m._interfaces
		})
		if info, ok := interfaces[from.Zone]; ok && info.listen {
			addr.Zone = ""
			pin := fmt.Sprintf("/?key=%s", hex.EncodeToString(key))
			u, err := url.Parse("tls://" + addr.String() + pin)
			if err != nil {
				m.log.Debugln("Call from multicast failed, parse error:", addr.String(), err)
			}
			if err := m.core.CallPeer(u, from.Zone); err != nil {
				m.log.Debugln("Call from multicast failed:", err)
			}
		}
	}
}
