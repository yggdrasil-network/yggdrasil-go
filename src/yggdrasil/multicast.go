package yggdrasil

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"time"

	"golang.org/x/net/ipv6"
)

type multicast struct {
	core        *Core
	reconfigure chan chan error
	sock        *ipv6.PacketConn
	groupAddr   string
	listeners   map[string]*tcpListener
	listenPort  uint16
}

func (m *multicast) init(core *Core) {
	m.core = core
	m.reconfigure = make(chan chan error, 1)
	m.listeners = make(map[string]*tcpListener)
	current, _ := m.core.config.Get()
	m.listenPort = current.LinkLocalTCPPort
	go func() {
		for {
			e := <-m.reconfigure
			e <- nil
		}
	}()
	m.groupAddr = "[ff02::114]:9001"
	// Check if we've been given any expressions
	if count := len(m.interfaces()); count != 0 {
		m.core.log.Infoln("Found", count, "multicast interface(s)")
	}
}

func (m *multicast) start() error {
	if len(m.interfaces()) == 0 {
		m.core.log.Infoln("Multicast discovery is disabled")
	} else {
		m.core.log.Infoln("Multicast discovery is enabled")
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
		if err = m.sock.SetControlMessage(ipv6.FlagDst, true); err != nil {
			// Windows can't set this flag, so we need to handle it in other ways
		}

		go m.multicastStarted()
		go m.listen()
		go m.announce()
	}
	return nil
}

func (m *multicast) interfaces() map[string]net.Interface {
	// Get interface expressions from config
	current, _ := m.core.config.Get()
	exprs := current.MulticastInterfaces
	// Ask the system for network interfaces
	interfaces := make(map[string]net.Interface)
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

func (m *multicast) announce() {
	groupAddr, err := net.ResolveUDPAddr("udp6", m.groupAddr)
	if err != nil {
		panic(err)
	}
	destAddr, err := net.ResolveUDPAddr("udp6", m.groupAddr)
	if err != nil {
		panic(err)
	}
	for {
		interfaces := m.interfaces()
		// There might be interfaces that we configured listeners for but are no
		// longer up - if that's the case then we should stop the listeners
		for name, listener := range m.listeners {
			// Prepare our stop function!
			stop := func() {
				listener.stop <- true
				delete(m.listeners, name)
				m.core.log.Debugln("No longer multicasting on", name)
			}
			// If the interface is no longer visible on the system then stop the
			// listener, as another one will be started further down
			if _, ok := interfaces[name]; !ok {
				stop()
				continue
			}
			// It's possible that the link-local listener address has changed so if
			// that is the case then we should clean up the interface listener
			found := false
			listenaddr, err := net.ResolveTCPAddr("tcp6", listener.listener.Addr().String())
			if err != nil {
				stop()
				continue
			}
			// Find the interface that matches the listener
			if intf, err := net.InterfaceByName(name); err == nil {
				if addrs, err := intf.Addrs(); err == nil {
					// Loop through the addresses attached to that listener and see if any
					// of them match the current address of the listener
					for _, addr := range addrs {
						if ip, _, err := net.ParseCIDR(addr.String()); err == nil {
							// Does the interface address match our listener address?
							if ip.Equal(listenaddr.IP) {
								found = true
								break
							}
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
		for _, iface := range interfaces {
			// Find interface addresses
			addrs, err := iface.Addrs()
			if err != nil {
				panic(err)
			}
			for _, addr := range addrs {
				addrIP, _, _ := net.ParseCIDR(addr.String())
				// Ignore IPv4 addresses
				if addrIP.To4() != nil {
					continue
				}
				// Ignore non-link-local addresses
				if !addrIP.IsLinkLocalUnicast() {
					continue
				}
				// Join the multicast group
				m.sock.JoinGroup(&iface, groupAddr)
				// Try and see if we already have a TCP listener for this interface
				var listener *tcpListener
				if l, ok := m.listeners[iface.Name]; !ok || l.listener == nil {
					// No listener was found - let's create one
					listenaddr := fmt.Sprintf("[%s%%%s]:%d", addrIP, iface.Name, m.listenPort)
					if li, err := m.core.link.tcp.listen(listenaddr); err == nil {
						m.core.log.Debugln("Started multicasting on", iface.Name)
						// Store the listener so that we can stop it later if needed
						m.listeners[iface.Name] = li
						listener = li
					} else {
						m.core.log.Warnln("Not multicasting on", iface.Name, "due to error:", err)
					}
				} else {
					// An existing listener was found
					listener = m.listeners[iface.Name]
				}
				// Make sure nothing above failed for some reason
				if listener == nil {
					continue
				}
				// Get the listener details and construct the multicast beacon
				lladdr := listener.listener.Addr().String()
				if a, err := net.ResolveTCPAddr("tcp6", lladdr); err == nil {
					a.Zone = ""
					destAddr.Zone = iface.Name
					msg := []byte(a.String())
					m.sock.WriteTo(msg, nil, destAddr)
				}
				break
			}
		}
		time.Sleep(time.Second * 15)
	}
}

func (m *multicast) listen() {
	groupAddr, err := net.ResolveUDPAddr("udp6", m.groupAddr)
	if err != nil {
		panic(err)
	}
	bs := make([]byte, 2048)
	for {
		nBytes, rcm, fromAddr, err := m.sock.ReadFrom(bs)
		if err != nil {
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
		addr.Zone = ""
		if err := m.core.link.call("tcp://"+addr.String(), from.Zone); err != nil {
			m.core.log.Debugln("Call from multicast failed:", err)
		}
	}
}
