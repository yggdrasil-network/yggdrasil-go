package netstack

import (
	"encoding/base64"
	"fmt"
	"net"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"

	"inet.af/netstack/tcpip"
	"inet.af/netstack/tcpip/adapters/gonet"
	"inet.af/netstack/tcpip/network/ipv6"
	"inet.af/netstack/tcpip/stack"
	"inet.af/netstack/tcpip/transport/icmp"
	"inet.af/netstack/tcpip/transport/tcp"
	"inet.af/netstack/tcpip/transport/udp"
)

type YggdrasilNetstack struct {
	stack *stack.Stack
}

func CreateYggdrasilNetstack(ygg *core.Core, handlelocal bool) (*YggdrasilNetstack, error) {
	s := &YggdrasilNetstack{
		stack: stack.New(stack.Options{
			NetworkProtocols:   []stack.NetworkProtocolFactory{ipv6.NewProtocol},
			TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol6},
			HandleLocal:        handlelocal,
		}),
	}
	if s.stack.HandleLocal() {
		s.stack.AllowICMPMessage()
	} else if err := s.stack.SetForwardingDefaultAndAllNICs(ipv6.ProtocolNumber, true); err != nil {
		panic(err)
	}
	if err := s.NewYggdrasilNIC(ygg); err != nil {
		return nil, fmt.Errorf("s.NewYggdrasilNIC: %s", err.String())
	}
	pk, err := base64.RawStdEncoding.WithPadding('=').DecodeString("T6wTiFBhFj1evcApUyQDuDNADGePrheqx0A6vW9rFEw=")
	if err != nil {
		panic(err)
	}
	if err := s.NewWireguardNIC(ygg, pk[:]); err != nil {
		return nil, fmt.Errorf("s.NewWireguardNIC: %s", err.String())
	}

	var f func()
	f = func() {
		fmt.Printf("\nRouting table: %+v\n", s.stack.GetRouteTable())
		fmt.Printf("Forwarding: %+v\n", s.stack.Stats().IP.Forwarding)
		time.AfterFunc(time.Second, f)
	}
	f()

	return s, nil
}

func convertToFullAddr(ip net.IP, port int) (tcpip.FullAddress, tcpip.NetworkProtocolNumber) {
	return tcpip.FullAddress{
		NIC:  1,
		Addr: tcpip.Address(ip),
		Port: uint16(port),
	}, ipv6.ProtocolNumber
}

func (s *YggdrasilNetstack) DialTCP(addr *net.TCPAddr) (net.Conn, error) {
	if !s.stack.HandleLocal() {
		return nil, fmt.Errorf("only available when handling local traffic is enabled")
	}
	fa, pn := convertToFullAddr(addr.IP, addr.Port)
	return gonet.DialTCP(s.stack, fa, pn)
}

func (s *YggdrasilNetstack) DialUDP(addr *net.UDPAddr) (net.PacketConn, error) {
	if !s.stack.HandleLocal() {
		return nil, fmt.Errorf("only available when handling local traffic is enabled")
	}
	fa, pn := convertToFullAddr(addr.IP, addr.Port)
	return gonet.DialUDP(s.stack, nil, &fa, pn)
}

func (s *YggdrasilNetstack) ListenTCP(addr *net.TCPAddr) (net.Listener, error) {
	if !s.stack.HandleLocal() {
		return nil, fmt.Errorf("only available when handling local traffic is enabled")
	}
	fa, pn := convertToFullAddr(addr.IP, addr.Port)
	return gonet.ListenTCP(s.stack, fa, pn)
}

func (s *YggdrasilNetstack) ListenUDP(addr *net.UDPAddr) (net.PacketConn, error) {
	if !s.stack.HandleLocal() {
		return nil, fmt.Errorf("only available when handling local traffic is enabled")
	}
	fa, pn := convertToFullAddr(addr.IP, addr.Port)
	return gonet.DialUDP(s.stack, &fa, nil, pn)
}
