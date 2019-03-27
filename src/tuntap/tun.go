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

	"github.com/songgao/packets/ethernet"
	"github.com/yggdrasil-network/water"

	"github.com/yggdrasil-network/yggdrasil-go/src/adapter"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

const tun_IPv6_HEADER_LENGTH = 40
const tun_ETHER_HEADER_LENGTH = 14

// Represents a running TUN/TAP interface.
type TunAdapter struct {
	adapter.Adapter
	addr   address.Address
	subnet address.Subnet
	Icmpv6 icmpv6
	mtu    int
	iface  *water.Interface
	mutex  sync.RWMutex // Protects the below
	isOpen bool
}

// Gets the maximum supported MTU for the platform based on the defaults in
// defaults.GetDefaults().
func GetSupportedMTU(mtu int) int {
	if mtu > defaults.GetDefaults().MaximumIfMTU {
		return defaults.GetDefaults().MaximumIfMTU
	}
	return mtu
}

func (tun *TunAdapter) GetMTU() int {
	return GetSupportedMTU(tun.mtu)
}

func (tun *TunAdapter) GetTAPMode() bool {
	return tun.iface.IsTAP()
}

func (tun *TunAdapter) GetName() string {
	return tun.iface.Name()
}

// Initialises the TUN/TAP adapter.
func (tun *TunAdapter) Init(config *config.StatefulNodeConfig, log *log.Logger, send chan<- []byte, recv <-chan []byte) {
	tun.Adapter.Init(config, log, send, recv)
	tun.Icmpv6.init(tun)
	go func() {
		for {
			e := <-tun.Reconfigure
			tun.Config.Mutex.RLock()
			updated := tun.Config.Current.IfName != tun.Config.Previous.IfName ||
				tun.Config.Current.IfTAPMode != tun.Config.Previous.IfTAPMode ||
				tun.Config.Current.IfMTU != tun.Config.Previous.IfMTU
			tun.Config.Mutex.RUnlock()
			if updated {
				tun.Log.Warnln("Reconfiguring TUN/TAP is not supported yet")
				e <- nil
			} else {
				e <- nil
			}
		}
	}()
}

// Starts the setup process for the TUN/TAP adapter, and if successful, starts
// the read/write goroutines to handle packets on that interface.
func (tun *TunAdapter) Start(addrobj address.Address, subnetobj address.Subnet) error {
	tun.addr = addrobj
	tun.subnet = subnetobj
	tun.Config.Mutex.RLock()
	ifname := tun.Config.Current.IfName
	iftapmode := tun.Config.Current.IfTAPMode
	addr := fmt.Sprintf("%s/%d", net.IP(tun.addr[:]).String(), 8*len(address.GetPrefix())-1)
	mtu := tun.Config.Current.IfMTU
	tun.Config.Mutex.RUnlock()
	if ifname != "none" {
		if err := tun.Setup(ifname, iftapmode, addr, mtu); err != nil {
			return err
		}
	}
	if ifname == "none" || ifname == "dummy" {
		return nil
	}
	tun.mutex.Lock()
	tun.isOpen = true
	tun.mutex.Unlock()
	go func() { tun.Log.Errorln("WARNING: tun.Read() exited with error:", tun.Read()) }()
	go func() { tun.Log.Errorln("WARNING: tun.Write() exited with error:", tun.Write()) }()
	if iftapmode {
		go func() {
			for {
				if _, ok := tun.Icmpv6.peermacs[tun.addr]; ok {
					break
				}
				request, err := tun.Icmpv6.create_ndp_tap(tun.addr)
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
		data := <-tun.Recv
		if tun.iface == nil {
			continue
		}
		if tun.iface.IsTAP() {
			var destAddr address.Address
			if data[0]&0xf0 == 0x60 {
				if len(data) < 40 {
					panic("Tried to send a packet shorter than an IPv6 header...")
				}
				copy(destAddr[:16], data[24:])
			} else if data[0]&0xf0 == 0x40 {
				if len(data) < 20 {
					panic("Tried to send a packet shorter than an IPv4 header...")
				}
				copy(destAddr[:4], data[16:])
			} else {
				return errors.New("Invalid address family")
			}
			sendndp := func(destAddr address.Address) {
				neigh, known := tun.Icmpv6.peermacs[destAddr]
				known = known && (time.Since(neigh.lastsolicitation).Seconds() < 30)
				if !known {
					request, err := tun.Icmpv6.create_ndp_tap(destAddr)
					if err != nil {
						panic(err)
					}
					if _, err := tun.iface.Write(request); err != nil {
						panic(err)
					}
					tun.Icmpv6.peermacs[destAddr] = neighbor{
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
			if neighbor, ok := tun.Icmpv6.peermacs[destAddr]; ok && neighbor.learned {
				peermac = neighbor.mac
				peerknown = true
			} else if neighbor, ok := tun.Icmpv6.peermacs[tun.addr]; ok && neighbor.learned {
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
					tun.Icmpv6.mymac[:6], // Source MAC address
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
				// panic(err)
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
				go tun.Icmpv6.parse_packet(b)
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
