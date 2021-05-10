package tuntap

import (
	"crypto/ed25519"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"

	//"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	//"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"

	//"golang.org/x/net/icmp"
	//"golang.org/x/net/ipv6"

	iwt "github.com/Arceliar/ironwood/types"
	//"github.com/Arceliar/phony"
)

const TUN_OFFSET_BYTES = 4

func (tun *TunAdapter) read() {
	var buf [TUN_OFFSET_BYTES + 65535]byte
	for {
		n, err := tun.iface.Read(buf[:], TUN_OFFSET_BYTES)
		if n <= TUN_OFFSET_BYTES || err != nil {
			tun.log.Errorln("Error reading TUN:", err)
			ferr := tun.iface.Flush()
			if ferr != nil {
				tun.log.Errorln("Unable to flush packets:", ferr)
			}
			return
		}
		begin := TUN_OFFSET_BYTES
		end := begin + n
		bs := buf[begin:end]
		if bs[0]&0xf0 != 0x60 {
			continue // not IPv6
		}
		if len(bs) < 40 {
			tun.log.Traceln("TUN iface read undersized ipv6 packet, length:", len(bs))
			continue
		}
		var srcAddr, dstAddr address.Address
		var srcSubnet, dstSubnet address.Subnet
		copy(srcAddr[:], bs[8:])
		copy(dstAddr[:], bs[24:])
		copy(srcSubnet[:], bs[8:])
		copy(dstSubnet[:], bs[24:])
		if srcAddr != tun.addr && srcSubnet != tun.subnet {
			continue // Wrong soruce address
		}
		if dstAddr.IsValid() {
			tun.store.sendToAddress(dstAddr, bs)
		} else if dstSubnet.IsValid() {
			tun.store.sendToSubnet(dstSubnet, bs)
		}
	}
}

func (tun *TunAdapter) write() {
	var buf [TUN_OFFSET_BYTES + 65535]byte
	for {
		bs := buf[TUN_OFFSET_BYTES:]
		n, from, err := tun.core.ReadFrom(bs)
		if err != nil {
			return
		}
		bs = bs[:n]
		if bs[0]&0xf0 != 0x60 {
			continue // not IPv6
		}
		if len(bs) < 40 {
			continue
		}
		tun.log.Println(len(bs), tun.MTU())
		if len(bs) > int(tun.MTU()) {
			ptb := &icmp.PacketTooBig{
				MTU:  int(tun.mtu),
				Data: bs[:40],
			}
			if packet, err := CreateICMPv6(bs[8:24], bs[24:40], ipv6.ICMPTypePacketTooBig, 0, ptb); err == nil {
				_, _ = tun.core.WriteTo(packet, from)
			}
			continue
		}
		var srcAddr, dstAddr address.Address
		var srcSubnet, dstSubnet address.Subnet
		copy(srcAddr[:], bs[8:])
		copy(dstAddr[:], bs[24:])
		copy(srcSubnet[:], bs[8:])
		copy(dstSubnet[:], bs[24:])
		if dstAddr != tun.addr && dstSubnet != tun.subnet {
			continue // bad local address/subnet
		}
		info := tun.store.update(ed25519.PublicKey(from.(iwt.Addr)))
		if srcAddr != info.address && srcSubnet != info.subnet {
			continue // bad remote address/subnet
		}
		bs = buf[:TUN_OFFSET_BYTES+n]
		n, err = tun.iface.Write(bs, TUN_OFFSET_BYTES)
		if err != nil {
			tun.Act(nil, func() {
				if !tun.isOpen {
					tun.log.Errorln("TUN iface write error:", err)
				}
			})
		}
		if n != len(bs) {
			// TODO some kind of error reporting for a partial write
		}
	}
}
