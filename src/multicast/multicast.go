package multicast

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"time"

	"github.com/Arceliar/phony"
	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
	"golang.org/x/net/ipv6"
)

const (
	// GroupAddr contains the multicast group and port used for multicast packets.
	GroupAddr = "[ff02::114]:9001"
)

// Multicast represents the multicast advertisement and discovery mechanism used
// by Yggdrasil to find peers on the same subnet. When a beacon is received on a
// configured multicast interface, Yggdrasil will attempt to peer with that node
// automatically.
type Multicast struct {
	phony.Inbox
	core             *yggdrasil.Core
	config           *config.NodeState
	log              *log.Logger
	sock             *ipv6.PacketConn
	groupAddr        *net.UDPAddr
	listeners        map[string]*multicastInterface
	listenPort       uint16
	isOpen           bool
	interfaceMonitor *time.Timer
	announcer        *time.Timer
	platformhandler  *time.Timer
}

type multicastInterface struct {
	phony.Inbox
	sock     *ipv6.PacketConn
	destAddr net.UDPAddr
	listener *yggdrasil.TcpListener
	zone     string
	timer    *time.Timer
	interval time.Duration
	send     chan<- beacon
	stop     chan interface{}
}

type beacon struct {
	llAddr string
	zone   string
}

// Init prepares the multicast interface for use.
func (m *Multicast) Init(core *yggdrasil.Core, state *config.NodeState, log *log.Logger, options interface{}) (err error) {
	m.core = core
	m.config = state
	m.log = log
	m.listeners = make(map[string]*multicastInterface)
	current := m.config.GetCurrent()
	m.listenPort = current.LinkLocalTCPPort
	m.groupAddr, err = net.ResolveUDPAddr("udp6", GroupAddr)
	return
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
	if len(m.config.GetCurrent().MulticastInterfaces) == 0 {
		return nil
	}
	m.log.Infoln("Starting multicast module")
	addr, err := net.ResolveUDPAddr("udp", GroupAddr)
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
	if err = m.sock.SetControlMessage(ipv6.FlagDst, true); err != nil {
		// Windows can't set this flag, so we need to handle it in other ways
	}

	m.isOpen = true
	go m.listen()
	m.Act(m, m.multicastStarted)
	m.Act(m, m.monitorInterfaceChanges)

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
	/*
		if m.monitorInterfaceChanges != nil {
			m.monitorInterfaceChanges.Stop()
		}
		if m.sendBeacons != nil {
			m.sendBeacons.Stop()
		}
	*/
	if m.platformhandler != nil {
		m.platformhandler.Stop()
	}
	if m.sock != nil {
		m.sock.Close()
	}
	return nil
}

// UpdateConfig updates the multicast module with the provided config.NodeConfig
// and then signals the various module goroutines to reconfigure themselves if
// needed.
func (m *Multicast) UpdateConfig(config *config.NodeConfig) {
	m.Act(m, func() { m._updateConfig(config) })
}

func (m *Multicast) _updateConfig(config *config.NodeConfig) {
	m.log.Infoln("Reloading multicast configuration...")
	if m.isOpen {
		if len(config.MulticastInterfaces) == 0 || config.LinkLocalTCPPort != m.listenPort {
			if err := m._stop(); err != nil {
				m.log.Errorln("Error stopping multicast module:", err)
			}
		}
	}
	m.config.Replace(*config)
	m.listenPort = config.LinkLocalTCPPort
	if !m.isOpen && len(config.MulticastInterfaces) > 0 {
		if err := m._start(); err != nil {
			m.log.Errorln("Error starting multicast module:", err)
		}
	}
	m.log.Debugln("Reloaded multicast configuration successfully")
}

func (m *Multicast) monitorInterfaceChanges() {
	interfaces := m.Interfaces()

	// Look for interfaces we don't know about yet.
	for name, intf := range interfaces {
		if _, ok := m.listeners[name]; !ok {
			// Look up interface addresses.
			addrs, err := intf.Addrs()
			if err != nil {
				continue
			}
			// Find the first link-local address.
			for _, addr := range addrs {
				addrIP, _, _ := net.ParseCIDR(addr.String())
				// Join the multicast group.
				m.sock.JoinGroup(&intf, m.groupAddr)
				// Construct a listener on this address.
				listenaddr := fmt.Sprintf("[%s%%%s]:%d", addrIP, intf.Name, m.listenPort)
				listener, err := m.core.ListenTCP(listenaddr)
				if err != nil {
					m.log.Warnln("Not multicasting on", name, "due to error:", err)
					continue
				}
				// This is a new interface. Start an announcer for it.
				multicastInterface := &multicastInterface{
					sock:     m.sock,
					destAddr: *m.groupAddr,
					listener: listener,
					stop:     make(chan interface{}),
					zone:     name,
				}
				multicastInterface.Act(multicastInterface, multicastInterface.announce)
				m.listeners[name] = multicastInterface
				m.log.Infoln("Started multicasting on", name)
				break
			}
		}
	}
	// Look for interfaces we knew about but are no longer there.
	for name, intf := range m.listeners {
		if _, ok := interfaces[name]; !ok {
			// This is a disappeared interface. Stop the announcer.
			close(intf.stop)
			delete(m.listeners, name)
			m.log.Infoln("Stopped multicasting on", name)
		}
	}
	// Queue the next check.
	m.interfaceMonitor = time.AfterFunc(time.Second, func() {
		m.Act(m, m.monitorInterfaceChanges)
	})
}

func (m *multicastInterface) announce() {
	// Check if the multicast interface has been stopped. This will happen
	// if it disappears from the system or goes down.
	select {
	case <-m.stop:
		return
	default:
	}
	// Send the beacon.
	lladdr := m.listener.Listener.Addr().String()
	if a, err := net.ResolveTCPAddr("tcp6", lladdr); err == nil {
		a.Zone = ""
		msg := []byte(a.String())
		m.sock.WriteTo(msg, nil, &m.destAddr)
	}
	// Queue the next beacon.
	if m.interval.Seconds() < 15 {
		m.interval += time.Second
	}
	m.timer = time.AfterFunc(m.interval, func() {
		m.Act(m, m.announce)
	})
}

// GetInterfaces returns the currently known/enabled multicast interfaces. It is
// expected that UpdateInterfaces has been called at least once before calling
// this method.
func (m *Multicast) Interfaces() map[string]net.Interface {
	interfaces := make(map[string]net.Interface)
	// Get interface expressions from config
	current := m.config.GetCurrent()
	exprs := current.MulticastInterfaces
	// Ask the system for network interfaces
	allifaces, err := net.Interfaces()
	if err != nil {
		panic(err)
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
		addrs, _ := iface.Addrs()
		hasLLAddr := false
		for _, addr := range addrs {
			addrIP, _, _ := net.ParseCIDR(addr.String())
			if addrIP.To4() == nil && addrIP.IsLinkLocalUnicast() {
				hasLLAddr = true
				break
			}
		}
		if !hasLLAddr {
			// Ignore interfaces without link-local addresses
			continue
		}
		for _, expr := range exprs {
			// Compile each regular expression
			e, err := regexp.Compile(expr)
			if err != nil {
				panic(err)
			}
			// Does the interface match the regular expression? Store it if so
			if e.MatchString(iface.Name) {
				interfaces[iface.Name] = iface
			}
		}
	}
	return interfaces
}

func (m *Multicast) listen() {
	groupAddr, err := net.ResolveUDPAddr("udp6", GroupAddr)
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
		anAddr := string(bs[:nBytes])
		addr, err := net.ResolveTCPAddr("tcp6", anAddr)
		if err != nil {
			continue
		}
		from := fromAddr.(*net.UDPAddr)
		if addr.IP.String() != from.IP.String() {
			continue
		}
		if _, ok := m.Interfaces()[from.Zone]; ok {
			addr.Zone = ""
			if err := m.core.CallPeer("tcp://"+addr.String(), from.Zone); err != nil {
				m.log.Debugln("Call from multicast failed:", err)
			}
		}
	}
}
