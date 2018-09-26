package yggdrasil

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/net/ipv6"
)

type multicast struct {
	core      *Core
	sock      *ipv6.PacketConn
	groupAddr string
}

func (m *multicast) init(core *Core) {
	m.core = core
	m.groupAddr = "[ff02::114]:9001"
	// Check if we've been given any expressions
	if len(m.core.ifceExpr) == 0 {
		return
	}
	// Ask the system for network interfaces
	m.core.log.Println("Found", len(m.interfaces()), "multicast interface(s)")
}

func (m *multicast) start() error {
	if len(m.core.ifceExpr) == 0 {
		m.core.log.Println("Multicast discovery is disabled")
	} else {
		m.core.log.Println("Multicast discovery is enabled")
		addr, err := net.ResolveUDPAddr("udp", m.groupAddr)
		if err != nil {
			return err
		}
		listenString := fmt.Sprintf("[::]:%v", addr.Port)
		conn, err := net.ListenPacket("udp6", listenString)
		if err != nil {
			return err
		}
		m.sock = ipv6.NewPacketConn(conn)
		if err = m.sock.SetControlMessage(ipv6.FlagDst, true); err != nil {
			// Windows can't set this flag, so we need to handle it in other ways
		}

		go m.listen()
		go m.announce()
	}
	return nil
}

func (m *multicast) interfaces() []net.Interface {
	// Ask the system for network interfaces
	var interfaces []net.Interface
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
		for _, expr := range m.core.ifceExpr {
			if expr.MatchString(iface.Name) {
				interfaces = append(interfaces, iface)
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
	var anAddr net.TCPAddr
	myAddr := m.core.tcp.getAddr()
	anAddr.Port = myAddr.Port
	destAddr, err := net.ResolveUDPAddr("udp6", m.groupAddr)
	if err != nil {
		panic(err)
	}
	for {
		for _, iface := range m.interfaces() {
			m.sock.JoinGroup(&iface, groupAddr)
			addrs, err := iface.Addrs()
			if err != nil {
				panic(err)
			}
			for _, addr := range addrs {
				addrIP, _, _ := net.ParseCIDR(addr.String())
				if addrIP.To4() != nil {
					continue
				} // IPv6 only
				if !addrIP.IsLinkLocalUnicast() {
					continue
				}
				anAddr.IP = addrIP
				anAddr.Zone = iface.Name
				destAddr.Zone = iface.Name
				msg := []byte(anAddr.String())
				m.sock.WriteTo(msg, nil, destAddr)
				break
			}
			time.Sleep(time.Second)
		}
		time.Sleep(time.Second)
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
		addr.Zone = from.Zone
		saddr := addr.String()
		m.core.tcp.connect(saddr, "")
	}
}
