package tuntap

import (
	"crypto/ed25519"
	"sync"
	"time"

	iwt "github.com/Arceliar/ironwood/types"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

const keyStoreTimeout = 2 * time.Minute

type keyStore struct {
	tun          *TunAdapter
	mutex        sync.Mutex
	keyToInfo    map[keyArray]*keyInfo
	addrToInfo   map[address.Address]*keyInfo
	addrBuffer   map[address.Address]*buffer
	subnetToInfo map[address.Subnet]*keyInfo
	subnetBuffer map[address.Subnet]*buffer
}

type keyArray [ed25519.PublicKeySize]byte

type keyInfo struct {
	key     keyArray
	address address.Address
	subnet  address.Subnet
	mtu     MTU         // TODO use this
	timeout *time.Timer // From calling a time.AfterFunc to do cleanup
}

type buffer struct {
	packets [][]byte
	timeout *time.Timer
}

func (k *keyStore) init(tun *TunAdapter) {
	k.tun = tun
	k.keyToInfo = make(map[keyArray]*keyInfo)
	k.addrToInfo = make(map[address.Address]*keyInfo)
	k.addrBuffer = make(map[address.Address]*buffer)
	k.subnetToInfo = make(map[address.Subnet]*keyInfo)
	k.subnetBuffer = make(map[address.Subnet]*buffer)
}

func (k *keyStore) sendToAddress(addr address.Address, bs []byte) {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	if info := k.addrToInfo[addr]; info != nil {
		k.tun.core.WriteTo(bs, iwt.Addr(info.key[:]))
		k.resetTimeout(info)
	} else {
		var buf *buffer
		if buf = k.addrBuffer[addr]; buf == nil {
			buf = new(buffer)
			k.addrBuffer[addr] = buf
		}
		msg := append([]byte(nil), bs...)
		buf.packets = append(buf.packets, msg)
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
		panic("TODO") // TODO send lookup
	}
}

func (k *keyStore) sendToSubnet(subnet address.Subnet, bs []byte) {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	if info := k.subnetToInfo[subnet]; info != nil {
		k.tun.core.WriteTo(bs, iwt.Addr(info.key[:]))
		k.resetTimeout(info)
	} else {
		var buf *buffer
		if buf = k.subnetBuffer[subnet]; buf == nil {
			buf = new(buffer)
			k.subnetBuffer[subnet] = buf
		}
		msg := append([]byte(nil), bs...)
		buf.packets = append(buf.packets, msg)
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
		panic("TODO") // TODO send lookup
	}
}

func (k *keyStore) update(key ed25519.PublicKey) *keyInfo {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	var kArray keyArray
	copy(kArray[:], key)
	var info *keyInfo
	if info = k.keyToInfo[kArray]; info == nil {
		info = new(keyInfo)
		info.key = kArray
		info.address = *address.AddrForKey(ed25519.PublicKey(info.key[:]))
		info.subnet = *address.SubnetForKey(ed25519.PublicKey(info.key[:]))
		info.mtu = MTU(^uint16(0)) // TODO
		k.keyToInfo[info.key] = info
		k.addrToInfo[info.address] = info
		k.subnetToInfo[info.subnet] = info
		k.resetTimeout(info)
		if buf := k.addrBuffer[info.address]; buf != nil {
			for _, bs := range buf.packets {
				k.tun.core.WriteTo(bs, iwt.Addr(info.key[:]))
			}
			delete(k.addrBuffer, info.address)
		}
		if buf := k.subnetBuffer[info.subnet]; buf != nil {
			for _, bs := range buf.packets {
				k.tun.core.WriteTo(bs, iwt.Addr(info.key[:]))
			}
			delete(k.subnetBuffer, info.subnet)
		}
	}
	k.resetTimeout(info)
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
