package ipv6rwc

// The ICMPv6 module implements functions to easily create ICMPv6
// packets. These functions, when mixed with the built-in Go IPv6
// and ICMP libraries, can be used to send control messages back
// to the host. Examples include:
// - NDP messages, when running in TAP mode
// - Packet Too Big messages, when packets exceed the session MTU
// - Destination Unreachable messages, when a session prohibits
//   incoming traffic

import (
	"encoding/binary"
	"net"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
)

type ICMPv6 struct{}

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

// Creates an ICMPv6 packet based on the given icmp.MessageBody and other
// parameters, complete with IP headers only, which can be written directly to
// a TUN adapter, or called directly by the CreateICMPv6L2 function when
// generating a message for TAP adapters.
func CreateICMPv6(dst net.IP, src net.IP, mtype ipv6.ICMPType, mcode int, mbody icmp.MessageBody) ([]byte, error) {
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
