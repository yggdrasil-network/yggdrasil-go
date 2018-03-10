package yggdrasil

// The NDP functions are needed when you are running with a
// TAP adapter - as the operating system expects neighbor solicitations
// for on-link traffic, this goroutine provides them

import "net"
import "golang.org/x/net/ipv6"
import "golang.org/x/net/icmp"
import "encoding/binary"
import "errors"

type macAddress [6]byte

const ETHER = 14

type icmpv6 struct {
	tun        *tunDevice
	peermac    macAddress
	peerlladdr net.IP
	mylladdr   net.IP
	mymac      macAddress
}

// Marshal returns the binary encoding of h.
func ipv6Header_Marshal(h *ipv6.Header) ([]byte, error) {
	b := make([]byte, 40)
	b[0] |= byte(h.Version) << 4
	b[0] |= byte(h.TrafficClass) >> 4
	b[1] |= byte(h.TrafficClass) << 4
	b[1] |= byte(h.FlowLabel >> 16)
	b[2] = byte(h.FlowLabel >> 8)
	b[3] = byte(h.FlowLabel)
	binary.BigEndian.PutUint16(b[4:6], uint16(h.PayloadLen))
	b[6] = byte(h.NextHeader)
	b[7] = byte(h.HopLimit)
	copy(b[8:24], h.Src)
	copy(b[24:40], h.Dst)
	return b, nil
}

func (i *icmpv6) init(t *tunDevice) {
	i.tun = t

	// Our MAC address and link-local address
	copy(i.mymac[:], []byte{
		0x02, 0x00, 0x00, 0x00, 0x00, 0x02})
	i.mylladdr = net.IP{
		0xFE, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80, 0xFE}
}

func (i *icmpv6) parse_packet(datain []byte) {
	var response []byte
	var err error

	// Parse the frame/packet
	if i.tun.iface.IsTAP() {
		response, err = i.parse_packet_tap(datain)
	} else {
		response, err = i.parse_packet_tun(datain)
	}

	if err != nil {
		return
	}

	// Write the packet to TUN/TAP
	i.tun.iface.Write(response)
}

func (i *icmpv6) parse_packet_tap(datain []byte) ([]byte, error) {
	// Store the peer MAC address
	copy(i.peermac[:6], datain[6:12])

	// Ignore non-IPv6 frames
	if binary.BigEndian.Uint16(datain[12:14]) != uint16(0x86DD) {
		return nil, nil
	}

	// Hand over to parse_packet_tun to interpret the IPv6 packet
	ipv6packet, err := i.parse_packet_tun(datain[ETHER:])
	if err != nil {
		return nil, err
	}

	// Create the response buffer
	dataout := make([]byte, ETHER+ipv6.HeaderLen+32)

	// Populate the response ethernet headers
	copy(dataout[:6], datain[6:12])
	copy(dataout[6:12], i.mymac[:])
	binary.BigEndian.PutUint16(dataout[12:14], uint16(0x86DD))

	// Copy the returned packet to our response ethernet frame
	copy(dataout[ETHER:], ipv6packet)
	return dataout, nil
}

func (i *icmpv6) parse_packet_tun(datain []byte) ([]byte, error) {
	// Parse the IPv6 packet headers
	ipv6Header, err := ipv6.ParseHeader(datain[:ipv6.HeaderLen])
	if err != nil {
		return nil, err
	}

	// Check if the packet is IPv6
	if ipv6Header.Version != ipv6.Version {
		return nil, err
	}

	// Check if the packet is ICMPv6
	if ipv6Header.NextHeader != 58 {
		return nil, err
	}

	// Store the peer link local address, it will come in useful later
	copy(i.peerlladdr[:], ipv6Header.Src[:])

	// Parse the ICMPv6 message contents
	icmpv6Header, err := icmp.ParseMessage(58, datain[ipv6.HeaderLen:])
	if err != nil {
		return nil, err
	}

	// Check for a supported message type
	switch icmpv6Header.Type {
	case ipv6.ICMPTypeNeighborSolicitation:
		{
			response, err := i.handle_ndp(datain[ipv6.HeaderLen:])
			if err == nil {
				// Create our ICMPv6 response
				responsePacket, err := i.create_icmpv6_tun(
					ipv6Header.Src, i.mylladdr,
					ipv6.ICMPTypeNeighborAdvertisement, 0,
					&icmp.DefaultMessageBody{Data: response})
				if err != nil {
					return nil, err
				}

				// Send it back
				return responsePacket, nil
			} else {
				return nil, err
			}
		}
	}

	return nil, errors.New("ICMPv6 type not matched")
}

func (i *icmpv6) create_icmpv6_tap(dstmac macAddress, dst net.IP, src net.IP, mtype ipv6.ICMPType, mcode int, mbody icmp.MessageBody) ([]byte, error) {
	// Pass through to create_icmpv6_tun
	ipv6packet, err := i.create_icmpv6_tun(dst, src, mtype, mcode, mbody)
	if err != nil {
		return nil, err
	}

	// Create the response buffer
	dataout := make([]byte, ETHER+len(ipv6packet))

	// Populate the response ethernet headers
	copy(dataout[:6], dstmac[:6])
	copy(dataout[6:12], i.mymac[:])
	binary.BigEndian.PutUint16(dataout[12:14], uint16(0x86DD))

	// Copy the returned packet to our response ethernet frame
	copy(dataout[ETHER:], ipv6packet)
	return dataout, nil
}

func (i *icmpv6) create_icmpv6_tun(dst net.IP, src net.IP, mtype ipv6.ICMPType, mcode int, mbody icmp.MessageBody) ([]byte, error) {
	// Create the ICMPv6 message
	icmpMessage := icmp.Message{
		Type: mtype,
		Code: mcode,
		Body: mbody,
	}

	// Convert the ICMPv6 message into []byte
	icmpMessageBuf, err := icmpMessage.Marshal(icmp.IPv6PseudoHeader(src, dst))
	if err != nil {
		return nil, err
	}

	// Create the IPv6 header
	ipv6Header := ipv6.Header{
		Version:    ipv6.Version,
		NextHeader: 58,
		PayloadLen: len(icmpMessageBuf),
		HopLimit:   255,
		Src:        src,
		Dst:        dst,
	}

	// Convert the IPv6 header into []byte
	ipv6HeaderBuf, err := ipv6Header_Marshal(&ipv6Header)
	if err != nil {
		return nil, err
	}

	// Construct the packet
	responsePacket := make([]byte, ipv6.HeaderLen+ipv6Header.PayloadLen)
	copy(responsePacket[:ipv6.HeaderLen], ipv6HeaderBuf)
	copy(responsePacket[ipv6.HeaderLen:], icmpMessageBuf)

	// Send it back
	return responsePacket, nil
}

func (i *icmpv6) handle_ndp(in []byte) ([]byte, error) {
	// Ignore NDP requests for anything outside of fd00::/8
	if in[8] != 0xFD {
		return nil, errors.New("Not an NDP for fd00::/8")
	}

	// Create our NDP message body response
	body := make([]byte, 28)
	binary.BigEndian.PutUint32(body[:4], uint32(0x20000000))
	copy(body[4:20], in[8:24]) // Target address
	body[20] = uint8(2)
	body[21] = uint8(1)
	copy(body[22:28], i.mymac[:6])

	// Send it back
	return body, nil
}
