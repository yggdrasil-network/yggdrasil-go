package tuntap

// This manages the tun driver to send/recv packets to/from applications

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gologme/log"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"

	"github.com/songgao/packets/ethernet"
	"github.com/yggdrasil-network/water"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

const tun_IPv6_HEADER_LENGTH = 40
const tun_ETHER_HEADER_LENGTH = 14

// Represents a running TUN/TAP interface.
type TunAdapter struct {
	yggdrasil.Adapter
	addr   address.Address
	subnet address.Subnet
	log    *log.Logger
	config *config.NodeState
	icmpv6 ICMPv6
	mtu    int
	iface  *water.Interface
	mutex  sync.RWMutex // Protects the below
	isOpen bool
}

// Gets the maximum supported MTU for the platform based on the defaults in
// defaults.GetDefaults().
func getSupportedMTU(mtu int) int {
	if mtu > defaults.GetDefaults().MaximumIfMTU {
		return defaults.GetDefaults().MaximumIfMTU
	}
	return mtu
}

// Get the adapter name
func (tun *TunAdapter) Name() string {
	return tun.iface.Name()
}

// Get the adapter MTU
func (tun *TunAdapter) MTU() int {
	return getSupportedMTU(tun.mtu)
}

// Get the adapter mode
func (tun *TunAdapter) IsTAP() bool {
	return tun.iface.IsTAP()
}

// Gets the default TUN/TAP interface name for your platform.
func DefaultName() string {
	return defaults.GetDefaults().DefaultIfName
}

// Gets the default TUN/TAP interface MTU for your platform. This can be as high
// as 65535, depending on platform, but is never lower than 1280.
func DefaultMTU() int {
	return defaults.GetDefaults().DefaultIfMTU
}

// Gets the default TUN/TAP interface mode for your platform.
func DefaultIsTAP() bool {
	return defaults.GetDefaults().DefaultIfTAPMode
}

// Gets the maximum supported TUN/TAP interface MTU for your platform. This
// can be as high as 65535, depending on platform, but is never lower than 1280.
func MaximumMTU() int {
	return defaults.GetDefaults().MaximumIfMTU
}

// Initialises the TUN/TAP adapter.
func (tun *TunAdapter) Init(config *config.NodeState, log *log.Logger, send chan<- []byte, recv <-chan []byte, reject <-chan yggdrasil.RejectedPacket) {
	tun.config = config
	tun.log = log
	tun.Adapter.Init(config, log, send, recv, reject)
	tun.icmpv6.Init(tun)
	go func() {
		for {
			e := <-tun.Reconfigure
			tun.config.Mutex.RLock()
			updated := tun.config.Current.IfName != tun.config.Previous.IfName ||
				tun.config.Current.IfTAPMode != tun.config.Previous.IfTAPMode ||
				tun.config.Current.IfMTU != tun.config.Previous.IfMTU
			tun.config.Mutex.RUnlock()
			if updated {
				tun.log.Warnln("Reconfiguring TUN/TAP is not supported yet")
				e <- nil
			} else {
				e <- nil
			}
		}
	}()
}

// Starts the setup process for the TUN/TAP adapter, and if successful, starts
// the read/write goroutines to handle packets on that interface.
func (tun *TunAdapter) Start(a address.Address, s address.Subnet) error {
	tun.addr = a
	tun.subnet = s
	if tun.config == nil {
		return errors.New("No configuration available to TUN/TAP")
	}
	tun.config.Mutex.RLock()
	ifname := tun.config.Current.IfName
	iftapmode := tun.config.Current.IfTAPMode
	addr := fmt.Sprintf("%s/%d", net.IP(tun.addr[:]).String(), 8*len(address.GetPrefix())-1)
	mtu := tun.config.Current.IfMTU
	tun.config.Mutex.RUnlock()
	if ifname != "none" {
		if err := tun.setup(ifname, iftapmode, addr, mtu); err != nil {
			return err
		}
	}
	if ifname == "none" || ifname == "dummy" {
		tun.log.Debugln("Not starting TUN/TAP as ifname is none or dummy")
		return nil
	}
	tun.mutex.Lock()
	tun.isOpen = true
	tun.mutex.Unlock()
	go func() {
		tun.log.Debugln("Starting TUN/TAP reader goroutine")
		tun.log.Errorln("WARNING: tun.read() exited with error:", tun.Read())
	}()
	go func() {
		tun.log.Debugln("Starting TUN/TAP writer goroutine")
		tun.log.Errorln("WARNING: tun.write() exited with error:", tun.Write())
	}()
	if iftapmode {
		go func() {
			for {
				if _, ok := tun.icmpv6.peermacs[tun.addr]; ok {
					break
				}
				request, err := tun.icmpv6.CreateNDPL2(tun.addr)
				if err != nil {
					panic(err)
				}
				if _, err := tun.iface.Write(request); err != nil {
					panic(err)
				}
				time.Sleep(time.Second)
			}
		}()
	}
	return nil
}

// Writes a packet to the TUN/TAP adapter. If the adapter is running in TAP
// mode then additional ethernet encapsulation is added for the benefit of the
// host operating system.
func (tun *TunAdapter) Write() error {
	for {
		select {
		case reject := <-tun.Reject:
			switch reject.Reason {
			case yggdrasil.PacketTooBig:
				if mtu, ok := reject.Detail.(int); ok {
					// Create the Packet Too Big response
					ptb := &icmp.PacketTooBig{
						MTU:  int(mtu),
						Data: reject.Packet,
					}

					// Create the ICMPv6 response from it
					icmpv6Buf, err := CreateICMPv6(
						reject.Packet[8:24], reject.Packet[24:40],
						ipv6.ICMPTypePacketTooBig, 0, ptb)

					// Send the ICMPv6 response back to the TUN/TAP adapter
					if err == nil {
						tun.iface.Write(icmpv6Buf)
					}
				}
				fallthrough
			default:
				continue
			}
		case data := <-tun.Recv:
			if tun.iface == nil {
				continue
			}
			if tun.iface.IsTAP() {
				var destAddr address.Address
				if data[0]&0xf0 == 0x60 {
					if len(data) < 40 {
						//panic("Tried to send a packet shorter than an IPv6 header...")
						util.PutBytes(data)
						continue
					}
					copy(destAddr[:16], data[24:])
				} else if data[0]&0xf0 == 0x40 {
					if len(data) < 20 {
						//panic("Tried to send a packet shorter than an IPv4 header...")
						util.PutBytes(data)
						continue
					}
					copy(destAddr[:4], data[16:])
				} else {
					return errors.New("Invalid address family")
				}
				sendndp := func(destAddr address.Address) {
					neigh, known := tun.icmpv6.peermacs[destAddr]
					known = known && (time.Since(neigh.lastsolicitation).Seconds() < 30)
					if !known {
						request, err := tun.icmpv6.CreateNDPL2(destAddr)
						if err != nil {
							panic(err)
						}
						if _, err := tun.iface.Write(request); err != nil {
							panic(err)
						}
						tun.icmpv6.peermacs[destAddr] = neighbor{
							lastsolicitation: time.Now(),
						}
					}
				}
				var peermac macAddress
				var peerknown bool
				if data[0]&0xf0 == 0x40 {
					destAddr = tun.addr
				} else if data[0]&0xf0 == 0x60 {
					if !bytes.Equal(tun.addr[:16], destAddr[:16]) && !bytes.Equal(tun.subnet[:8], destAddr[:8]) {
						destAddr = tun.addr
					}
				}
				if neighbor, ok := tun.icmpv6.peermacs[destAddr]; ok && neighbor.learned {
					peermac = neighbor.mac
					peerknown = true
				} else if neighbor, ok := tun.icmpv6.peermacs[tun.addr]; ok && neighbor.learned {
					peermac = neighbor.mac
					peerknown = true
					sendndp(destAddr)
				} else {
					sendndp(tun.addr)
				}
				if peerknown {
					var proto ethernet.Ethertype
					switch {
					case data[0]&0xf0 == 0x60:
						proto = ethernet.IPv6
					case data[0]&0xf0 == 0x40:
						proto = ethernet.IPv4
					}
					var frame ethernet.Frame
					frame.Prepare(
						peermac[:6],          // Destination MAC address
						tun.icmpv6.mymac[:6], // Source MAC address
						ethernet.NotTagged,   // VLAN tagging
						proto,                // Ethertype
						len(data))            // Payload length
					copy(frame[tun_ETHER_HEADER_LENGTH:], data[:])
					if _, err := tun.iface.Write(frame); err != nil {
						tun.mutex.RLock()
						open := tun.isOpen
						tun.mutex.RUnlock()
						if !open {
							return nil
						} else {
							panic(err)
						}
					}
				}
			} else {
				if _, err := tun.iface.Write(data); err != nil {
					tun.mutex.RLock()
					open := tun.isOpen
					tun.mutex.RUnlock()
					if !open {
						return nil
					} else {
						panic(err)
					}
				}
			}
			util.PutBytes(data)
		}
	}
}

// Reads any packets that are waiting on the TUN/TAP adapter. If the adapter
// is running in TAP mode then the ethernet headers will automatically be
// processed and stripped if necessary. If an ICMPv6 packet is found, then
// the relevant helper functions in icmpv6.go are called.
func (tun *TunAdapter) Read() error {
	mtu := tun.mtu
	if tun.iface.IsTAP() {
		mtu += tun_ETHER_HEADER_LENGTH
	}
	buf := make([]byte, mtu)
	for {
		n, err := tun.iface.Read(buf)
		if err != nil {
			tun.mutex.RLock()
			open := tun.isOpen
			tun.mutex.RUnlock()
			if !open {
				return nil
			} else {
				return err
			}
		}
		o := 0
		if tun.iface.IsTAP() {
			o = tun_ETHER_HEADER_LENGTH
		}
		switch {
		case buf[o]&0xf0 == 0x60 && n == 256*int(buf[o+4])+int(buf[o+5])+tun_IPv6_HEADER_LENGTH+o:
		case buf[o]&0xf0 == 0x40 && n == 256*int(buf[o+2])+int(buf[o+3])+o:
		default:
			continue
		}
		if buf[o+6] == 58 {
			if tun.iface.IsTAP() {
				// Found an ICMPv6 packet
				b := make([]byte, n)
				copy(b, buf)
				go tun.icmpv6.ParsePacket(b)
			}
		}
		packet := append(util.GetBytes(), buf[o:n]...)
		tun.Send <- packet
	}
}

// Closes the TUN/TAP adapter. This is only usually called when the Yggdrasil
// process stops. Typically this operation will happen quickly, but on macOS
// it can block until a read operation is completed.
func (tun *TunAdapter) Close() error {
	tun.mutex.Lock()
	tun.isOpen = false
	tun.mutex.Unlock()
	if tun.iface == nil {
		return nil
	}
	return tun.iface.Close()
}
