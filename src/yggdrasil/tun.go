package yggdrasil

// This manages the tun driver to send/recv packets to/from applications

import ethernet "github.com/songgao/packets/ethernet"

const IPv6_HEADER_LENGTH = 40
const ETHER_HEADER_LENGTH = 14

type tunInterface interface {
	IsTUN() bool
	IsTAP() bool
	Name() string
	Read(to []byte) (int, error)
	Write(from []byte) (int, error)
	Close() error
}

type tunDevice struct {
	core   *Core
	icmpv6 icmpv6
	send   chan<- []byte
	recv   <-chan []byte
	mtu    int
	iface  tunInterface
}

func (tun *tunDevice) init(core *Core) {
	tun.core = core
	tun.icmpv6.init(tun)
}

func (tun *tunDevice) write() error {
	for {
		data := <-tun.recv
		if tun.iface.IsTAP() {
			var frame ethernet.Frame
			frame.Prepare(
				tun.icmpv6.peermac[:6], // Destination MAC address
				tun.icmpv6.mymac[:6],   // Source MAC address
				ethernet.NotTagged,     // VLAN tagging
				ethernet.IPv6,          // Ethertype
				len(data))              // Payload length
			copy(frame[ETHER_HEADER_LENGTH:], data[:])
			if _, err := tun.iface.Write(frame); err != nil {
				panic(err)
			}
		} else {
			if _, err := tun.iface.Write(data); err != nil {
				panic(err)
			}
		}
		util_putBytes(data)
	}
}

func (tun *tunDevice) read() error {
	mtu := tun.mtu
	if tun.iface.IsTAP() {
		mtu += ETHER_HEADER_LENGTH
	}
	buf := make([]byte, mtu)
	for {
		n, err := tun.iface.Read(buf)
		if err != nil {
			panic(err)
		}
		o := 0
		if tun.iface.IsTAP() {
			o = ETHER_HEADER_LENGTH
		}
		if buf[o]&0xf0 != 0x60 ||
			n != 256*int(buf[o+4])+int(buf[o+5])+IPv6_HEADER_LENGTH+o {
			// Either not an IPv6 packet or not the complete packet for some reason
			//panic("Should not happen in testing")
			continue
		}
		if buf[o+6] == 58 {
			// Found an ICMPv6 packet
			b := make([]byte, n)
			copy(b, buf)
			// tun.icmpv6.recv <- b
			if tun.iface.IsTAP() {
				go tun.icmpv6.parse_packet_tap(b)
			} else {
				go tun.icmpv6.parse_packet_tun(b)
			}
		}
		packet := append(util_getBytes(), buf[o:n]...)
		tun.send <- packet
	}
}

func (tun *tunDevice) close() error {
	return tun.iface.Close()
}
