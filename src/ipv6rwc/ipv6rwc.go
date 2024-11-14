package ipv6rwc

import (
	"crypto/ed25519"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"

	iwt "github.com/Arceliar/ironwood/types"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

const keyStoreTimeout = 2 * time.Minute

type keyArray [ed25519.PublicKeySize]byte

type keyStore struct {
	core         *core.Core
	address      address.Address
	subnet       address.Subnet
	mutex        sync.Mutex
	keyToInfo    map[keyArray]*keyInfo
	addrToInfo   map[address.Address]*keyInfo
	addrBuffer   map[address.Address]*buffer
	subnetToInfo map[address.Subnet]*keyInfo
	subnetBuffer map[address.Subnet]*buffer
	tunnelHelper TunnelHelper
	mtu          uint64
}

type keyInfo struct {
	key     keyArray
	address address.Address
	subnet  address.Subnet
	timeout *time.Timer // From calling a time.AfterFunc to do cleanup
}

type buffer struct {
	packet  []byte
	timeout *time.Timer
}

func (k *keyStore) init(c *core.Core) {
	k.core = c
	k.address = *address.AddrForKey(k.core.PublicKey())
	k.subnet = *address.SubnetForKey(k.core.PublicKey())
	k.core.SetPathNotify(func(key ed25519.PublicKey) {
		k.update(key)
	})
	k.keyToInfo = make(map[keyArray]*keyInfo)
	k.addrToInfo = make(map[address.Address]*keyInfo)
	k.addrBuffer = make(map[address.Address]*buffer)
	k.subnetToInfo = make(map[address.Subnet]*keyInfo)
	k.subnetBuffer = make(map[address.Subnet]*buffer)
	k.mtu = 1280 // Default to something safe, expect user to set this
}

func (k *keyStore) sendToAddress(addr address.Address, bs []byte) {
	k.mutex.Lock()
	if info := k.addrToInfo[addr]; info != nil {
		k.resetTimeout(info)
		k.mutex.Unlock()
		_, _ = k.core.WriteTo(bs, iwt.Addr(info.key[:]))
	} else {
		var buf *buffer
		if buf = k.addrBuffer[addr]; buf == nil {
			buf = new(buffer)
			k.addrBuffer[addr] = buf
		}
		msg := append([]byte(nil), bs...)
		buf.packet = msg
		if buf.timeout != nil {
			buf.timeout.Stop()
		}
		buf.timeout = time.AfterFunc(keyStoreTimeout, func() {
			k.mutex.Lock()
			defer k.mutex.Unlock()
			if nbuf := k.addrBuffer[addr]; nbuf == buf {
				delete(k.addrBuffer, addr)
			}
		})
		k.mutex.Unlock()
		k.sendKeyLookup(addr.GetKey())
	}
}

func (k *keyStore) sendToSubnet(subnet address.Subnet, bs []byte) {
	k.mutex.Lock()
	if info := k.subnetToInfo[subnet]; info != nil {
		k.resetTimeout(info)
		k.mutex.Unlock()
		_, _ = k.core.WriteTo(bs, iwt.Addr(info.key[:]))
	} else {
		var buf *buffer
		if buf = k.subnetBuffer[subnet]; buf == nil {
			buf = new(buffer)
			k.subnetBuffer[subnet] = buf
		}
		msg := append([]byte(nil), bs...)
		buf.packet = msg
		if buf.timeout != nil {
			buf.timeout.Stop()
		}
		buf.timeout = time.AfterFunc(keyStoreTimeout, func() {
			k.mutex.Lock()
			defer k.mutex.Unlock()
			if nbuf := k.subnetBuffer[subnet]; nbuf == buf {
				delete(k.subnetBuffer, subnet)
			}
		})
		k.mutex.Unlock()
		k.sendKeyLookup(subnet.GetKey())
	}
}

func (k *keyStore) update(key ed25519.PublicKey) *keyInfo {
	k.mutex.Lock()
	var kArray keyArray
	copy(kArray[:], key)
	var info *keyInfo
	var packets [][]byte
	if info = k.keyToInfo[kArray]; info == nil {
		info = new(keyInfo)
		info.key = kArray
		info.address = *address.AddrForKey(ed25519.PublicKey(info.key[:]))
		info.subnet = *address.SubnetForKey(ed25519.PublicKey(info.key[:]))
		k.keyToInfo[info.key] = info
		k.addrToInfo[info.address] = info
		k.subnetToInfo[info.subnet] = info
		if buf := k.addrBuffer[info.address]; buf != nil {
			packets = append(packets, buf.packet)
			delete(k.addrBuffer, info.address)
		}
		if buf := k.subnetBuffer[info.subnet]; buf != nil {
			packets = append(packets, buf.packet)
			delete(k.subnetBuffer, info.subnet)
		}
	}
	k.resetTimeout(info)
	k.mutex.Unlock()
	for _, packet := range packets {
		_, _ = k.core.WriteTo(packet, iwt.Addr(info.key[:]))
	}
	return info
}

func (k *keyStore) resetTimeout(info *keyInfo) {
	if info.timeout != nil {
		info.timeout.Stop()
	}
	info.timeout = time.AfterFunc(keyStoreTimeout, func() {
		k.mutex.Lock()
		defer k.mutex.Unlock()
		if nfo := k.keyToInfo[info.key]; nfo == info {
			delete(k.keyToInfo, info.key)
		}
		if nfo := k.addrToInfo[info.address]; nfo == info {
			delete(k.addrToInfo, info.address)
		}
		if nfo := k.subnetToInfo[info.subnet]; nfo == info {
			delete(k.subnetToInfo, info.subnet)
		}
	})
}

func (k *keyStore) sendKeyLookup(partial ed25519.PublicKey) {
	k.core.SendLookup(partial)
}

func (k *keyStore) readPC(p []byte) (int, error) {
	buf := make([]byte, k.core.MTU(), 65535)
	for {
		bs := buf
		n, from, err := k.core.ReadFrom(bs)
		if err != nil {
			return n, err
		}
		if n == 0 {
			continue
		}
		bs = bs[:n]
		if len(bs) == 0 {
			continue
		}
		ip4 := bs[0]&0xf0 == 0x40
		ip6 := bs[0]&0xf0 == 0x60
		switch {
		case !ip4 && !ip6:
			continue
		case ip6 && len(bs) < 40:
			continue
		case ip4 && len(bs) < 20:
			continue
		}
		k.mutex.Lock()
		mtu := int(k.mtu)
		th := k.tunnelHelper
		k.mutex.Unlock()
		switch {
		case ip6 && len(bs) > mtu:
			// Using bs would make it leak off the stack, so copy to buf
			buf := make([]byte, 512)
			cn := copy(buf, bs)
			ptb := &icmp.PacketTooBig{
				MTU:  mtu,
				Data: buf[:cn],
			}
			if packet, err := CreateICMPv6(buf[8:24], buf[24:40], ipv6.ICMPTypePacketTooBig, 0, ptb); err == nil {
				_, _ = k.writePC(packet)
			}
			continue
		case len(bs) > mtu:
			continue
		}
		var srcAddr, dstAddr address.Address
		var srcSubnet, dstSubnet address.Subnet
		var addrlen int
		switch {
		case ip4:
			copy(srcAddr[:], bs[12:16])
			addrlen = 4
		case ip6:
			copy(srcAddr[:], bs[8:])
			copy(srcSubnet[:], bs[8:])
			copy(dstAddr[:], bs[24:])
			copy(dstSubnet[:], bs[24:])
			addrlen = 16
		}
		srcKey := ed25519.PublicKey(from.(iwt.Addr))
		info := k.update(srcKey)
		switch {
		case ip6 && (srcAddr == info.address || srcSubnet == info.subnet):
			return copy(p, bs), nil
		case ip4, ip6:
			if th == nil {
				continue
			}
			addr, ok := netip.AddrFromSlice(srcAddr[:addrlen])
			if !ok || !th.InboundAllowed(addr, srcKey) {
				continue
			}
		}
		return copy(p, bs), nil
	}
}

func (k *keyStore) writePC(bs []byte) (int, error) {
	if len(bs) == 0 {
		return 0, nil
	}
	ip4 := bs[0]&0xf0 == 0x40
	ip6 := bs[0]&0xf0 == 0x60
	switch {
	case !ip4 && !ip6:
		return len(bs), nil
	case ip6 && len(bs) < 40:
		return len(bs), nil
	case ip4 && len(bs) < 20:
		return len(bs), nil
	}
	var dstAddr address.Address
	var dstSubnet address.Subnet
	var addrlen int
	switch {
	case ip4:
		copy(dstAddr[:], bs[16:20])
		addrlen = 4
	case ip6:
		copy(dstAddr[:], bs[24:40])
		copy(dstSubnet[:], bs[24:40])
		addrlen = 16
	}
	switch {
	case dstAddr.IsValid():
		k.sendToAddress(dstAddr, bs)
	case dstSubnet.IsValid():
		k.sendToSubnet(dstSubnet, bs)
	default:
		k.mutex.Lock()
		th := k.tunnelHelper
		k.mutex.Unlock()
		if th == nil {
			return len(bs), nil
		}
		addr, ok := netip.AddrFromSlice(dstAddr[:addrlen])
		if !ok {
			return len(bs), nil
		}
		if key := th.OutboundAllowed(addr); key != nil && len(key) == ed25519.PublicKeySize {
			return k.core.WriteTo(bs, iwt.Addr(key))
		}
		return len(bs), nil
	}
	return len(bs), nil
}

// Exported API

func (k *keyStore) MaxMTU() uint64 {
	return k.core.MTU()
}

func (k *keyStore) SetMTU(mtu uint64) {
	if mtu > k.MaxMTU() {
		mtu = k.MaxMTU()
	}
	if mtu < 1280 {
		mtu = 1280
	}
	k.mutex.Lock()
	k.mtu = mtu
	k.mutex.Unlock()
}

func (k *keyStore) MTU() uint64 {
	k.mutex.Lock()
	mtu := k.mtu
	k.mutex.Unlock()
	return mtu
}

type ReadWriteCloser struct {
	keyStore
}

func NewReadWriteCloser(c *core.Core) *ReadWriteCloser {
	rwc := new(ReadWriteCloser)
	rwc.init(c)
	return rwc
}

func (rwc *ReadWriteCloser) Address() address.Address {
	return rwc.address
}

func (rwc *ReadWriteCloser) Subnet() address.Subnet {
	return rwc.subnet
}

func (rwc *ReadWriteCloser) Read(p []byte) (n int, err error) {
	return rwc.readPC(p)
}

func (rwc *ReadWriteCloser) Write(p []byte) (n int, err error) {
	return rwc.writePC(p)
}

func (rwc *ReadWriteCloser) Close() error {
	err := rwc.core.Close()
	rwc.core.Stop()
	return err
}

func (rwc *ReadWriteCloser) SetTunnelHelper(h TunnelHelper) {
	rwc.mutex.Lock()
	defer rwc.mutex.Unlock()
	rwc.tunnelHelper = h
}

type TunnelHelper interface {
	InboundAllowed(srcip netip.Addr, src ed25519.PublicKey) bool
	OutboundAllowed(dstip netip.Addr) ed25519.PublicKey
}
