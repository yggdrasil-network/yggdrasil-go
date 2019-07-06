package tuntap

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
	"errors"
	"net"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

const len_ETHER = 14

type ICMPv6 struct {
	tun           *TunAdapter
	mylladdr      net.IP
	mymac         net.HardwareAddr
	peermacs      map[address.Address]neighbor
	peermacsmutex sync.RWMutex
}

type neighbor struct {
	mac               net.HardwareAddr
	learned           bool
	lastadvertisement time.Time
	lastsolicitation  time.Time
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

// Initialises the ICMPv6 module by assigning our link-local IPv6 address and
// our MAC address. ICMPv6 messages will always appear to originate from these
// addresses.
func (i *ICMPv6) Init(t *TunAdapter) {
	i.tun = t
	i.peermacsmutex.Lock()
	i.peermacs = make(map[address.Address]neighbor)
	i.peermacsmutex.Unlock()

	// Our MAC address and link-local address
	i.mymac = net.HardwareAddr{
		0x02, 0x00, 0x00, 0x00, 0x00, 0x02}
	i.mylladdr = net.IP{
		0xFE, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80, 0xFE}
	copy(i.mymac[:], i.tun.addr[:])
	copy(i.mylladdr[9:], i.tun.addr[1:])
}

// Parses an incoming ICMPv6 packet. The packet provided may be either an
// ethernet frame containing an IP packet, or the IP packet alone. This is
// determined by whether the TUN/TAP adapter is running in TUN (layer 3) or
// TAP (layer 2) mode.
func (i *ICMPv6) ParsePacket(datain []byte) {
	var response []byte
	var err error

	// Parse the frame/packet
	if i.tun.IsTAP() {
		response, err = i.UnmarshalPacketL2(datain)
	} else {
		response, err = i.UnmarshalPacket(datain, nil)
	}

	if err != nil {
		return
	}

	// Write the packet to TUN/TAP
	i.tun.iface.Write(response)
}

// Unwraps the ethernet headers of an incoming ICMPv6 packet and hands off
// the IP packet to the ParsePacket function for further processing.
// A response buffer is also created for the response message, also complete
// with ethernet headers.
func (i *ICMPv6) UnmarshalPacketL2(datain []byte) ([]byte, error) {
	// Ignore non-IPv6 frames
	if binary.BigEndian.Uint16(datain[12:14]) != uint16(0x86DD) {
		return nil, nil
	}

	// Hand over to ParsePacket to interpret the IPv6 packet
	mac := datain[6:12]
	ipv6packet, err := i.UnmarshalPacket(datain[len_ETHER:], &mac)
	if err != nil {
		return nil, err
	}

	// Create the response buffer
	dataout := make([]byte, len_ETHER+ipv6.HeaderLen+32)

	// Populate the response ethernet headers
	copy(dataout[:6], datain[6:12])
	copy(dataout[6:12], i.mymac[:])
	binary.BigEndian.PutUint16(dataout[12:14], uint16(0x86DD))

	// Copy the returned packet to our response ethernet frame
	copy(dataout[len_ETHER:], ipv6packet)
	return dataout, nil
}

// Unwraps the IP headers of an incoming IPv6 packet and performs various
// sanity checks on the packet - i.e. is the packet an ICMPv6 packet, does the
// ICMPv6 message match a known expected type. The relevant handler function
// is then called and a response packet may be returned.
func (i *ICMPv6) UnmarshalPacket(datain []byte, datamac *[]byte) ([]byte, error) {
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

	// Parse the ICMPv6 message contents
	icmpv6Header, err := icmp.ParseMessage(58, datain[ipv6.HeaderLen:])
	if err != nil {
		return nil, err
	}

	// Check for a supported message type
	switch icmpv6Header.Type {
	case ipv6.ICMPTypeNeighborSolicitation:
		if !i.tun.IsTAP() {
			return nil, errors.New("Ignoring Neighbor Solicitation in TUN mode")
		}
		response, err := i.HandleNDP(datain[ipv6.HeaderLen:])
		if err == nil {
			// Create our ICMPv6 response
			responsePacket, err := CreateICMPv6(
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
	case ipv6.ICMPTypeNeighborAdvertisement:
		if !i.tun.IsTAP() {
			return nil, errors.New("Ignoring Neighbor Advertisement in TUN mode")
		}
		if datamac != nil {
			var addr address.Address
			var target address.Address
			mac := net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
			copy(addr[:], ipv6Header.Src[:])
			copy(target[:], datain[48:64])
			copy(mac[:], (*datamac)[:])
			i.peermacsmutex.Lock()
			neighbor := i.peermacs[target]
			neighbor.mac = mac
			neighbor.learned = true
			neighbor.lastadvertisement = time.Now()
			i.peermacs[target] = neighbor
			i.peermacsmutex.Unlock()
			i.tun.log.Debugln("Learned peer MAC", mac.String(), "for", net.IP(target[:]).String())
			/*
				i.tun.log.Debugln("Peer MAC table:")
				i.peermacsmutex.RLock()
				for t, n := range i.peermacs {
					if n.learned {
						i.tun.log.Debugln("- Target", net.IP(t[:]).String(), "has MAC", n.mac.String())
					} else {
						i.tun.log.Debugln("- Target", net.IP(t[:]).String(), "is not learned yet")
					}
				}
				i.peermacsmutex.RUnlock()
			*/
		}
		return nil, errors.New("No response needed")
	}

	return nil, errors.New("ICMPv6 type not matched")
}

// Creates an ICMPv6 packet based on the given icmp.MessageBody and other
// parameters, complete with ethernet and IP headers, which can be written
// directly to a TAP adapter.
func (i *ICMPv6) CreateICMPv6L2(dstmac net.HardwareAddr, dst net.IP, src net.IP, mtype ipv6.ICMPType, mcode int, mbody icmp.MessageBody) ([]byte, error) {
	// Pass through to CreateICMPv6
	ipv6packet, err := CreateICMPv6(dst, src, mtype, mcode, mbody)
	if err != nil {
		return nil, err
	}

	// Create the response buffer
	dataout := make([]byte, len_ETHER+len(ipv6packet))

	// Populate the response ethernet headers
	copy(dataout[:6], dstmac[:6])
	copy(dataout[6:12], i.mymac[:])
	binary.BigEndian.PutUint16(dataout[12:14], uint16(0x86DD))

	// Copy the returned packet to our response ethernet frame
	copy(dataout[len_ETHER:], ipv6packet)
	return dataout, nil
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

func (i *ICMPv6) Solicit(addr address.Address) {
	retries := 5
	for retries > 0 {
		retries--
		i.peermacsmutex.RLock()
		if n, ok := i.peermacs[addr]; ok && n.learned {
			i.tun.log.Debugln("MAC learned for", net.IP(addr[:]).String())
			i.peermacsmutex.RUnlock()
			return
		}
		i.peermacsmutex.RUnlock()
		i.tun.log.Debugln("Sending neighbor solicitation for", net.IP(addr[:]).String())
		i.peermacsmutex.Lock()
		if n, ok := i.peermacs[addr]; !ok {
			i.peermacs[addr] = neighbor{
				lastsolicitation: time.Now(),
			}
		} else {
			n.lastsolicitation = time.Now()
		}
		i.peermacsmutex.Unlock()
		request, err := i.createNDPL2(addr)
		if err != nil {
			panic(err)
		}
		if _, err := i.tun.iface.Write(request); err != nil {
			panic(err)
		}
		i.tun.log.Debugln("Sent neighbor solicitation for", net.IP(addr[:]).String())
		time.Sleep(time.Second)
	}
}

func (i *ICMPv6) createNDPL2(dst address.Address) ([]byte, error) {
	// Create the ND payload
	var payload [28]byte
	copy(payload[:4], []byte{0x00, 0x00, 0x00, 0x00}) // Flags
	copy(payload[4:20], dst[:])                       // Destination
	copy(payload[20:22], []byte{0x01, 0x01})          // Type & length
	copy(payload[22:28], i.mymac[:6])                 // Link layer address

	// Create the ICMPv6 solicited-node address
	var dstaddr address.Address
	copy(dstaddr[:13], []byte{
		0xFF, 0x02, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01, 0xFF})
	copy(dstaddr[13:], dst[13:16])

	// Create the multicast MAC
	dstmac := net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	copy(dstmac[:2], []byte{0x33, 0x33})
	copy(dstmac[2:6], dstaddr[12:16])

	// Create the ND request
	requestPacket, err := i.CreateICMPv6L2(
		dstmac, dstaddr[:], i.mylladdr,
		ipv6.ICMPTypeNeighborSolicitation, 0,
		&icmp.DefaultMessageBody{Data: payload[:]})
	if err != nil {
		return nil, err
	}

	return requestPacket, nil
}

// Generates a response to an NDP discovery packet. This is effectively called
// when the host operating system generates an NDP request for any address in
// the fd00::/8 range, so that the operating system knows to route that traffic
// to the Yggdrasil TAP adapter.
func (i *ICMPv6) HandleNDP(in []byte) ([]byte, error) {
	// Ignore NDP requests for anything outside of fd00::/8
	var source address.Address
	copy(source[:], in[8:])
	var snet address.Subnet
	copy(snet[:], in[8:])
	switch {
	case source.IsValid():
	case snet.IsValid():
	default:
		return nil, errors.New("Not an NDP for 0200::/7")
	}

	// Create our NDP message body response
	body := make([]byte, 28)
	binary.BigEndian.PutUint32(body[:4], uint32(0x40000000)) // Flags
	copy(body[4:20], in[8:24])                               // Target address
	body[20] = uint8(2)                                      // Type: Target link-layer address
	body[21] = uint8(1)                                      // Length: 1x address (8 bytes)
	copy(body[22:28], i.mymac[:6])

	// Send it back
	return body, nil
}
