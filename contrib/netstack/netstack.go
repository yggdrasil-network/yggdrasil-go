package netstack

import (
	"context"
	"fmt"
	"net"
	"strconv"

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

func CreateYggdrasilNetstack(ygg *core.Core) (*YggdrasilNetstack, error) {
	s := &YggdrasilNetstack{
		stack: stack.New(stack.Options{
			NetworkProtocols:   []stack.NetworkProtocolFactory{ipv6.NewProtocol},
			TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol6},
			HandleLocal:        true,
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
	return s, nil
}

func convertToFullAddr(ip net.IP, port int) (tcpip.FullAddress, tcpip.NetworkProtocolNumber, error) {
	return tcpip.FullAddress{
		NIC:  1,
		Addr: tcpip.Address(ip),
		Port: uint16(port),
	}, ipv6.ProtocolNumber, nil
}

func convertToFullAddrFromString(endpoint string) (tcpip.FullAddress, tcpip.NetworkProtocolNumber, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return tcpip.FullAddress{}, 0, fmt.Errorf("net.SplitHostPort: %w", err)
	}
	pn := 80
	if port != "" {
		if pn, err = strconv.Atoi(port); err != nil {
			return tcpip.FullAddress{}, 0, fmt.Errorf("strconv.Atoi: %w", err)
		}
	}
	return convertToFullAddr(net.ParseIP(host), pn)
}

func (s *YggdrasilNetstack) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	fa, pn, err := convertToFullAddrFromString(address)
	if err != nil {
		return nil, fmt.Errorf("convertToFullAddrFromString: %w", err)
	}
	switch network {
	case "tcp", "tcp6":
		return gonet.DialContextTCP(ctx, s.stack, fa, pn)
	case "udp", "udp6":
		return gonet.DialUDP(s.stack, nil, &fa, pn)
	default:
		return nil, fmt.Errorf("not supported")
	}
}

func (s *YggdrasilNetstack) DialTCP(addr *net.TCPAddr) (net.Conn, error) {
	fa, pn, _ := convertToFullAddr(addr.IP, addr.Port)
	return gonet.DialTCP(s.stack, fa, pn)
}

func (s *YggdrasilNetstack) DialUDP(addr *net.UDPAddr) (net.PacketConn, error) {
	fa, pn, _ := convertToFullAddr(addr.IP, addr.Port)
	return gonet.DialUDP(s.stack, nil, &fa, pn)
}

func (s *YggdrasilNetstack) ListenTCP(addr *net.TCPAddr) (net.Listener, error) {
	fa, pn, _ := convertToFullAddr(addr.IP, addr.Port)
	return gonet.ListenTCP(s.stack, fa, pn)
}

func (s *YggdrasilNetstack) ListenUDP(addr *net.UDPAddr) (net.PacketConn, error) {
	fa, pn, _ := convertToFullAddr(addr.IP, addr.Port)
	return gonet.DialUDP(s.stack, &fa, nil, pn)
}
