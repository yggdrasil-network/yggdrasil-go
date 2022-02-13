package netstack

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"golang.org/x/crypto/curve25519"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"

	"inet.af/netstack/tcpip"
	"inet.af/netstack/tcpip/buffer"
	"inet.af/netstack/tcpip/header"
	"inet.af/netstack/tcpip/network/ipv4"
	"inet.af/netstack/tcpip/network/ipv6"
	"inet.af/netstack/tcpip/stack"
)

type YggdrasilWireguard struct {
	stack          *YggdrasilNetstack
	device         *device.Device
	dispatcher     stack.NetworkDispatcher
	events         chan tun.Event
	incomingPacket chan buffer.VectorisedView
}

type YggdrasilWireguardEndpoint YggdrasilWireguard

func (s *YggdrasilNetstack) NewWireguardNIC(ygg *core.Core, public ed25519.PublicKey) tcpip.Error {
	wg := &YggdrasilWireguard{
		stack: s,
	}

	var nsk device.NoisePrivateKey
	var npk device.NoisePublicKey
	apk := (*[device.NoisePublicKeySize]byte)(&npk)
	ask := (*[device.NoisePrivateKeySize]byte)(&nsk)

	ysk := hex.EncodeToString(ygg.PrivateKey()[:ed25519.PrivateKeySize-ed25519.PublicKeySize])
	if err := nsk.FromMaybeZeroHex(ysk); err != nil {
		panic(err)
	}
	curve25519.ScalarBaseMult(apk, ask)

	wg.device = device.NewDevice(wg, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, ""))
	if err := wg.device.IpcSet(fmt.Sprintf(""+
		"listen_port=12346\n"+
		"private_key=%s\n"+
		"public_key=%s\n"+
		"allowed_ip=%s/128\n"+
		"allowed_ip=%s/64",
		hex.EncodeToString(ask[:]),
		hex.EncodeToString(public[:]),
		ygg.Address().String(),
		ygg.Subnet().IP.String(),
	)); err != nil {
		panic(err)
	}
	wg.device.Up()

	/*
		fmt.Println("WIREGUARD CONFIG:")
		fmt.Println()
		fmt.Println("[Interface]")
		fmt.Println("Address =", ygg.Address().String())
		fmt.Println()
		fmt.Println("[Peer]")
		fmt.Println("Endpoint = localhost:12346")
		fmt.Println("AllowedIPs = 200::/7")
		fmt.Println("PublicKey =", base64.RawStdEncoding.WithPadding('=').EncodeToString(apk[:]))
		fmt.Println()
	*/

	if err := s.stack.CreateNIC(2, (*YggdrasilWireguardEndpoint)(wg)); err != nil {
		return err
	}

	addr := ygg.Address()
	snet := ygg.Subnet()
	m := []byte{
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	}

	routeAddr, err := tcpip.NewSubnet(
		tcpip.Address(addr),
		tcpip.AddressMask(m[:]),
	)
	if err != nil {
		panic(err)
	}
	routeSnet, err := tcpip.NewSubnet(
		tcpip.Address(string(snet.IP)),
		tcpip.AddressMask(string(snet.Mask)),
	)
	if err != nil {
		panic(err)
	}
	s.stack.AddRoute(tcpip.Route{
		Destination: routeAddr,
		NIC:         2,
	})
	s.stack.AddRoute(tcpip.Route{
		Destination: routeSnet,
		NIC:         2,
	})
	return nil
}

//////////// BELOW IMPLEMENTS tcpip.Endpoint ////////////

func (e *YggdrasilWireguardEndpoint) Attach(dispatcher stack.NetworkDispatcher) {
	e.dispatcher = dispatcher
}

func (e *YggdrasilWireguardEndpoint) IsAttached() bool {
	return e.dispatcher != nil
}

func (e *YggdrasilWireguardEndpoint) MTU() uint32 {
	return 1420
}

func (*YggdrasilWireguardEndpoint) Capabilities() stack.LinkEndpointCapabilities {
	return stack.CapabilityNone
}

func (*YggdrasilWireguardEndpoint) MaxHeaderLength() uint16 {
	return 0
}

func (*YggdrasilWireguardEndpoint) LinkAddress() tcpip.LinkAddress {
	return ""
}

func (*YggdrasilWireguardEndpoint) Wait() {}

func (e *YggdrasilWireguardEndpoint) WritePacket(_ stack.RouteInfo, _ tcpip.NetworkProtocolNumber, pkt *stack.PacketBuffer) tcpip.Error {
	e.incomingPacket <- buffer.NewVectorisedView(pkt.Size(), pkt.Views())
	return nil
}

func (e *YggdrasilWireguardEndpoint) WritePackets(stack.RouteInfo, stack.PacketBufferList, tcpip.NetworkProtocolNumber) (int, tcpip.Error) {
	panic("not implemented")
}

func (e *YggdrasilWireguardEndpoint) WriteRawPacket(*stack.PacketBuffer) tcpip.Error {
	panic("not implemented")
}

func (*YggdrasilWireguardEndpoint) ARPHardwareType() header.ARPHardwareType {
	return header.ARPHardwareNone
}

func (e *YggdrasilWireguardEndpoint) AddHeader(tcpip.LinkAddress, tcpip.LinkAddress, tcpip.NetworkProtocolNumber, *stack.PacketBuffer) {
}

//////////// BELOW IMPLEMENTS tun.Device ////////////

func (tun *YggdrasilWireguard) Name() (string, error) {
	return "go", nil
}

func (tun *YggdrasilWireguard) File() *os.File {
	return nil
}

func (tun *YggdrasilWireguard) Events() chan tun.Event {
	return tun.events
}

func (tun *YggdrasilWireguard) Read(buf []byte, offset int) (int, error) {
	view, ok := <-tun.incomingPacket
	if !ok {
		return 0, os.ErrClosed
	}
	return view.Read(buf[offset:])
}

func (tun *YggdrasilWireguard) Write(buf []byte, offset int) (int, error) {
	packet := buf[offset:]
	if len(packet) == 0 {
		return 0, nil
	}

	pkb := stack.NewPacketBuffer(stack.PacketBufferOptions{Data: buffer.NewVectorisedView(len(packet), []buffer.View{buffer.NewViewFromBytes(packet)})})
	switch packet[0] >> 4 {
	case 4:
		tun.dispatcher.DeliverNetworkPacket("", "", ipv4.ProtocolNumber, pkb)
	case 6:
		tun.dispatcher.DeliverNetworkPacket("", "", ipv6.ProtocolNumber, pkb)
	}

	return len(buf), nil
}

func (tun *YggdrasilWireguard) Flush() error {
	return nil
}

func (tun *YggdrasilWireguard) Close() error {
	if tun.events != nil {
		close(tun.events)
	}
	if tun.incomingPacket != nil {
		close(tun.incomingPacket)
	}
	return nil
}

func (tun *YggdrasilWireguard) MTU() (int, error) {
	return 1420, nil
}
