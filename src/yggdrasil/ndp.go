package yggdrasil

// The NDP functions are needed when you are running with a
// TAP adapter - as the operating system expects neighbor solicitations
// for on-link traffic, this goroutine provides them

import "golang.org/x/net/icmp"
import "encoding/binary"
import "unsafe"

type macAddress [6]byte
type ipv6Address [16]byte

const ETHER = 14
const IPV6 = 40

type ndp struct {
	tun        *tunDevice
	peermac    macAddress
	peerlladdr ipv6Address
	mymac      macAddress
	mylladdr   ipv6Address
	recv       chan []byte
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

type icmpv6Packet struct {
	ether            etherHeader
	ipv6             ipv6Header
	icmpv6           icmpv6Header
	flags            [4]byte
	targetaddress    ipv6Address
	optiontype       byte
	optionlength     byte
	linklayeraddress macAddress
}

func (n *ndp) init(t *tunDevice) {
	n.tun = t
	n.recv = make(chan []byte)
	copy(n.mymac[:], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x02})
	copy(n.mylladdr[:], []byte{
		0xFE, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80, 0xFE})
	go n.listen()
}

func (n *ndp) listen() {
	for {
		// Receive from the channel and check if we're using TAP instead
		// of TUN mode - NDP is only relevant for TAP
		datain := <-n.recv
		if !n.tun.iface.IsTAP() {
			continue
		}

		// Create our return frame buffer and also the unsafe pointers to
		// map them to the structs
		dataout := make([]byte, ETHER+IPV6+32)
		in := (*icmpv6Packet)(unsafe.Pointer(&datain[0]))
		out := (*icmpv6Packet)(unsafe.Pointer(&dataout[0]))

		// Store peer MAC address and link-local IP address -
		// these will be used later by tun.go
		copy(n.peermac[:6], in.ether.source[:6])
		copy(n.peerlladdr[:16], in.ipv6.source[:16])

		// Ignore non-IPv6 packets
		if binary.BigEndian.Uint16(in.ether.ethertype[:]) != uint16(0x86DD) {
			continue
		}

		// Ignore non-ICMPv6 packets
		if in.ipv6.nextheader != uint8(0x3A) {
			continue
		}

		// Ignore non-NDP Solicitation packets
		if in.icmpv6.messagetype != uint8(135) {
			continue
		}

		// Ignore NDP requests for anything outside of fd00::/8
		if in.targetaddress[0] != 0xFD {
			continue
		}

		// Populate the out ethernet headers
		copy(out.ether.destination[:], in.ether.destination[:])
		copy(out.ether.source[:], n.mymac[:])
		binary.BigEndian.PutUint16(out.ether.ethertype[:], uint16(0x86DD))

		// And for now just copy the rest of the packet we were sent
		copy(dataout[ETHER:ETHER+IPV6], datain[ETHER:ETHER+IPV6])

		// Update the source and destination addresses in the IPv6 header
		copy(out.ipv6.destination[:], in.ipv6.source[:])
		copy(out.ipv6.source[:], n.mylladdr[:])
		binary.BigEndian.PutUint16(out.ipv6.length[:], uint16(32))

		// Copy the payload
		copy(dataout[ETHER+IPV6:], datain[ETHER+IPV6:])

		// Update the ICMPv6 headers
		out.icmpv6.messagetype = uint8(136)
		out.icmpv6.code = uint8(0)

		// Update the ICMPv6 payload
		copy(out.targetaddress[:], in.targetaddress[:])
		out.optiontype = uint8(2)
		out.optionlength = uint8(1)
		copy(out.linklayeraddress[:], n.mymac[:])
		binary.BigEndian.PutUint32(out.flags[:], uint32(0x20000000))

		// Generate the pseudo-header for the checksum
		ps := make([]byte, 44)
		pseudo := (*icmpv6PseudoHeader)(unsafe.Pointer(&ps[0]))
		copy(pseudo.destination[:], out.ipv6.destination[:])
		copy(pseudo.source[:], out.ipv6.source[:])
		binary.BigEndian.PutUint32(pseudo.length[:], uint32(binary.BigEndian.Uint16(out.ipv6.length[:])))
		pseudo.nextheader = out.ipv6.nextheader

		// Lazy-man's checksum using the icmp library
		icmpv6, err := icmp.ParseMessage(0x3A, dataout[ETHER+IPV6:])
		if err != nil {
			continue
		}
		payload, err := icmpv6.Marshal(ps)
		if err != nil {
			continue
		}
		copy(dataout[ETHER+IPV6:], payload)

		// Send the frame back to the TAP adapter
		n.tun.iface.Write(dataout)
	}
}
