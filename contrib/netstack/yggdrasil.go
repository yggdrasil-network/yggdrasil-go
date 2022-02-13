package netstack

import (
	"log"
	"net"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/ipv6rwc"

	"inet.af/netstack/tcpip"
	"inet.af/netstack/tcpip/buffer"
	"inet.af/netstack/tcpip/header"
	"inet.af/netstack/tcpip/network/ipv6"
	"inet.af/netstack/tcpip/stack"
)

type YggdrasilNIC struct {
	stack      *YggdrasilNetstack
	ipv6rwc    *ipv6rwc.ReadWriteCloser
	dispatcher stack.NetworkDispatcher
	readBuf    []byte
	writeBuf   []byte
}

func (s *YggdrasilNetstack) NewYggdrasilNIC(ygg *core.Core) tcpip.Error {
	rwc := ipv6rwc.NewReadWriteCloser(ygg)
	mtu := rwc.MTU()
	nic := &YggdrasilNIC{
		ipv6rwc:  rwc,
		readBuf:  make([]byte, mtu),
		writeBuf: make([]byte, mtu),
	}
	if err := s.stack.CreateNIC(1, nic); err != nil {
		return err
	}
	go func() {
		var rx int
		var err error
		for {
			rx, err = nic.ipv6rwc.Read(nic.readBuf)
			if err != nil {
				log.Println(err)
				break
			}
			pkb := stack.NewPacketBuffer(stack.PacketBufferOptions{
				Data: buffer.NewVectorisedView(rx, []buffer.View{
					buffer.NewViewFromBytes(nic.readBuf[:rx]),
				}),
			})
			nic.dispatcher.DeliverNetworkPacket("", "", ipv6.ProtocolNumber, pkb)
		}
	}()
	_, snet, err := net.ParseCIDR("0200::/7")
	if err != nil {
		return &tcpip.ErrBadAddress{}
	}
	subnet, err := tcpip.NewSubnet(
		tcpip.Address(string(snet.IP)),
		tcpip.AddressMask(string(snet.Mask)),
	)
	if err != nil {
		return &tcpip.ErrBadAddress{}
	}
	s.stack.AddRoute(tcpip.Route{
		Destination: subnet,
		NIC:         1,
	})
	if s.stack.HandleLocal() {
		ip := ygg.Address()
		if err := s.stack.AddProtocolAddress(
			1,
			tcpip.ProtocolAddress{
				Protocol:          ipv6.ProtocolNumber,
				AddressWithPrefix: tcpip.Address(ip).WithPrefix(),
			},
			stack.AddressProperties{},
		); err != nil {
			return err
		}
	}
	return nil
}

func (e *YggdrasilNIC) Attach(dispatcher stack.NetworkDispatcher) { e.dispatcher = dispatcher }

func (e *YggdrasilNIC) IsAttached() bool { return e.dispatcher != nil }

func (e *YggdrasilNIC) MTU() uint32 { return uint32(e.ipv6rwc.MTU()) }

func (*YggdrasilNIC) Capabilities() stack.LinkEndpointCapabilities { return stack.CapabilityNone }

func (*YggdrasilNIC) MaxHeaderLength() uint16 { return 40 }

func (*YggdrasilNIC) LinkAddress() tcpip.LinkAddress { return "" }

func (*YggdrasilNIC) Wait() {}

func (e *YggdrasilNIC) WritePacket(
	_ stack.RouteInfo,
	_ tcpip.NetworkProtocolNumber,
	pkt *stack.PacketBuffer,
) tcpip.Error {
	vv := buffer.NewVectorisedView(pkt.Size(), pkt.Views())
	n, err := vv.Read(e.writeBuf)
	if err != nil {
		log.Println(err)
		return &tcpip.ErrAborted{}
	}
	_, err = e.ipv6rwc.Write(e.writeBuf[:n])
	if err != nil {
		log.Println(err)
		return &tcpip.ErrAborted{}
	}
	return nil
}

func (e *YggdrasilNIC) WritePackets(
	stack.RouteInfo,
	stack.PacketBufferList,
	tcpip.NetworkProtocolNumber,
) (int, tcpip.Error) {
	panic("not implemented")
}

func (e *YggdrasilNIC) WriteRawPacket(*stack.PacketBuffer) tcpip.Error {
	panic("not implemented")
}

func (*YggdrasilNIC) ARPHardwareType() header.ARPHardwareType {
	return header.ARPHardwareNone
}

func (e *YggdrasilNIC) AddHeader(tcpip.LinkAddress, tcpip.LinkAddress, tcpip.NetworkProtocolNumber, *stack.PacketBuffer) {
}

func (e *YggdrasilNIC) Close() error {
	e.stack.stack.RemoveNIC(1)
	e.dispatcher = nil
	return nil
}
