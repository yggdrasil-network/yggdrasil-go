package yggdrasil

// This manages the tun driver to send/recv packets to/from applications

import (
	"yggdrasil/defaults"

	"github.com/songgao/packets/ethernet"
	"github.com/yggdrasil-network/water"
)

const tun_IPv6_HEADER_LENGTH = 40
const tun_ETHER_HEADER_LENGTH = 14

// Represents a running TUN/TAP interface.
type tunDevice struct {
	core   *Core
	icmpv6 icmpv6
	send   chan<- []byte
	recv   <-chan []byte
	mtu    int
	iface  *water.Interface
}

// Gets the maximum supported MTU for the platform based on the defaults in
// defaults.GetDefaults().
func getSupportedMTU(mtu int) int {
	if mtu > defaults.GetDefaults().MaximumIfMTU {
		return defaults.GetDefaults().MaximumIfMTU
	}
	return mtu
}

// Initialises the TUN/TAP adapter.
func (tun *tunDevice) init(core *Core) {
	tun.core = core
	tun.icmpv6.init(tun)
}

// Starts the setup process for the TUN/TAP adapter, and if successful, starts
// the read/write goroutines to handle packets on that interface.
func (tun *tunDevice) start(ifname string, iftapmode bool, addr string, mtu int) error {
	if ifname == "none" {
		return nil
	}
	if err := tun.setup(ifname, iftapmode, addr, mtu); err != nil {
		return err
	}
	go func() { panic(tun.read()) }()
	go func() { panic(tun.write()) }()
	return nil
}

// Writes a packet to the TUN/TAP adapter. If the adapter is running in TAP
// mode then additional ethernet encapsulation is added for the benefit of the
// host operating system.
func (tun *tunDevice) write() error {
	for {
		data := <-tun.recv
		if tun.iface == nil {
			continue
		}
		if tun.iface.IsTAP() {
			var frame ethernet.Frame
			frame.Prepare(
				tun.icmpv6.peermac[:6], // Destination MAC address
				tun.icmpv6.mymac[:6],   // Source MAC address
				ethernet.NotTagged,     // VLAN tagging
				ethernet.IPv6,          // Ethertype
				len(data))              // Payload length
			copy(frame[tun_ETHER_HEADER_LENGTH:], data[:])
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

// Reads any packets that are waiting on the TUN/TAP adapter. If the adapter
// is running in TAP mode then the ethernet headers will automatically be
// processed and stripped if necessary. If an ICMPv6 packet is found, then
// the relevant helper functions in icmpv6.go are called.
func (tun *tunDevice) read() error {
	mtu := tun.mtu
	if tun.iface.IsTAP() {
		mtu += tun_ETHER_HEADER_LENGTH
	}
	buf := make([]byte, mtu)
	for {
		n, err := tun.iface.Read(buf)
		if err != nil {
			// panic(err)
			return err
		}
		o := 0
		if tun.iface.IsTAP() {
			o = tun_ETHER_HEADER_LENGTH
		}
		if buf[o]&0xf0 != 0x60 ||
			n != 256*int(buf[o+4])+int(buf[o+5])+tun_IPv6_HEADER_LENGTH+o {
			// Either not an IPv6 packet or not the complete packet for some reason
			//panic("Should not happen in testing")
			//continue
		}
		if buf[o+6] == 58 {
			// Found an ICMPv6 packet
			b := make([]byte, n)
			copy(b, buf)
			// tun.icmpv6.recv <- b
			go tun.icmpv6.parse_packet(b)
		}
		packet := append(util_getBytes(), buf[o:n]...)
		tun.send <- packet
	}
}

// Closes the TUN/TAP adapter. This is only usually called when the Yggdrasil
// process stops. Typically this operation will happen quickly, but on macOS
// it can block until a read operation is completed.
func (tun *tunDevice) close() error {
	if tun.iface == nil {
		return nil
	}
	return tun.iface.Close()
}
