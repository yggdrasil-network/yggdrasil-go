package yggdrasil

// The NDP functions are needed when you are running with a
// TAP adapter - as the operating system expects neighbor solicitations
// for on-link traffic, this goroutine provides them

import "golang.org/x/net/icmp"
import "encoding/binary"
import "unsafe" // TODO investigate if this can be done without resorting to unsafe

type macAddress [6]byte
type ipv6Address [16]byte

const ETHER = 14
const IPV6 = 40

type icmpv6 struct {
	tun        *tunDevice
	peermac    macAddress
	peerlladdr ipv6Address
	mymac      macAddress
	mylladdr   ipv6Address
}

type etherHeader struct {
	destination macAddress
	source      macAddress
	ethertype   [2]byte
}

type ipv6Header struct {
	preamble    [4]byte
	length      [2]byte
	nextheader  byte
	hoplimit    byte
	source      ipv6Address
	destination ipv6Address
}

type icmpv6Header struct {
	messagetype byte
	code        byte
	checksum    uint16
}

type icmpv6PseudoHeader struct {
	source      ipv6Address
	destination ipv6Address
	length      [4]byte
	zero        [3]byte
	nextheader  byte
}

type icmpv6Payload struct {
	ether            etherHeader
	ipv6             ipv6Header
	icmpv6           icmpv6Header
	flags            [4]byte
	targetaddress    ipv6Address
	optiontype       byte
	optionlength     byte
	linklayeraddress macAddress
}

type icmpv6Packet struct {
	ipv6    ipv6Header
	payload icmpv6Payload
}

type icmpv6Frame struct {
	ether  etherHeader
	packet icmpv6Packet
}

func (i *icmpv6) init(t *tunDevice) {
	i.tun = t
	copy(i.mymac[:], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x02})
	copy(i.mylladdr[:], []byte{
		0xFE, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80, 0xFE})
}

func (i *icmpv6) parse_packet(datain []byte) {
	var response []byte
	var err error

	if i.tun.iface.IsTAP() {
		response, err = i.parse_packet_tap(datain)
	} else {
		response, err = i.parse_packet_tun(datain)
	}
	if err != nil {
		i.tun.core.log.Printf("ICMPv6 error: %v", err)
		return
	}
	if response != nil {
		i.tun.iface.Write(response)
	}
}

func (i *icmpv6) parse_packet_tap(datain []byte) ([]byte, error) {
	// Set up
	in := (*icmpv6Frame)(unsafe.Pointer(&datain[0]))

	// Store the peer MAC address
	copy(i.peermac[:6], in.ether.source[:6])

	// Ignore non-IPv6 frames
	if binary.BigEndian.Uint16(in.ether.ethertype[:]) != uint16(0x86DD) {
		return nil, nil
	}

	// Create the response buffer
	dataout := make([]byte, ETHER+IPV6+32)
	out := (*icmpv6Frame)(unsafe.Pointer(&dataout[0]))

	// Populate the response ethernet headers
	copy(out.ether.destination[:], in.ether.destination[:])
	copy(out.ether.source[:], i.mymac[:])
	binary.BigEndian.PutUint16(out.ether.ethertype[:], uint16(0x86DD))

	// Hand over to parse_packet_tun to interpret the IPv6 packet
	ipv6packet, err := i.parse_packet_tun(datain)
	if err != nil {
		return nil, nil
	}

	// Copy the returned packet to our response ethernet frame
	if ipv6packet != nil {
		copy(dataout[ETHER:ETHER+IPV6], ipv6packet)
		return dataout, nil
	}

	// At this point there is no response to send back
	return nil, nil
}

func (i *icmpv6) parse_packet_tun(datain []byte) ([]byte, error) {
	// Set up
	dataout := make([]byte, IPV6+32)
	out := (*icmpv6Packet)(unsafe.Pointer(&dataout[0]))
	in := (*icmpv6Packet)(unsafe.Pointer(&datain[0]))

	// Store the peer link-local address
	copy(i.peerlladdr[:16], in.ipv6.source[:16])

	// Ignore non-ICMPv6 packets
	if in.ipv6.nextheader != uint8(0x3A) {
		return nil, nil
	}

	// What is the ICMPv6 message type?
	switch in.payload.icmpv6.messagetype {
	case uint8(135):
		i.handle_ndp(&in.payload, &out.payload)
		break
	}

	// Update the source and destination addresses in the IPv6 header
	copy(out.ipv6.destination[:], in.ipv6.source[:])
	copy(out.ipv6.source[:], i.mylladdr[:])
	binary.BigEndian.PutUint16(out.ipv6.length[:], uint16(32))

	// Copy the payload
	copy(dataout[IPV6:], datain[IPV6:])

	// Calculate the checksum
	err := i.calculate_checksum(dataout)
	if err != nil {
		return nil, err
	}

	// Return the response packet
	return dataout, nil
}

func (i *icmpv6) calculate_checksum(dataout []byte) (error) {
	// Set up
	out := (*icmpv6Packet)(unsafe.Pointer(&dataout[0]))

	// Generate the pseudo-header for the checksum
	ps := make([]byte, 44)
	pseudo := (*icmpv6PseudoHeader)(unsafe.Pointer(&ps[0]))
	copy(pseudo.destination[:], out.ipv6.destination[:])
	copy(pseudo.source[:], out.ipv6.source[:])
	binary.BigEndian.PutUint32(pseudo.length[:], uint32(binary.BigEndian.Uint16(out.ipv6.length[:])))
	pseudo.nextheader = out.ipv6.nextheader

	// Lazy-man's checksum using the icmp library
	icmpv6, err := icmp.ParseMessage(0x3A, dataout[IPV6:])
	if err != nil {
		return err
	}

	// And copy the payload
	payload, err := icmpv6.Marshal(ps)
	if err != nil {
		return err
	}
	copy(dataout[IPV6:], payload)

	// Return nil if successful
	return nil
}

func (i *icmpv6) handle_ndp(in *icmpv6Payload, out *icmpv6Payload) {
	// Ignore NDP requests for anything outside of fd00::/8
	if in.targetaddress[0] != 0xFD {
		return
	}

	// Update the ICMPv6 headers
	out.icmpv6.messagetype = uint8(136)
	out.icmpv6.code = uint8(0)

	// Update the ICMPv6 payload
	copy(out.targetaddress[:], in.targetaddress[:])
	out.optiontype = uint8(2)
	out.optionlength = uint8(1)
	copy(out.linklayeraddress[:], i.mymac[:])
	binary.BigEndian.PutUint32(out.flags[:], uint32(0x20000000))
}
