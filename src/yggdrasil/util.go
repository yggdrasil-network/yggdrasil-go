package yggdrasil

// These are misc. utility functions that didn't really fit anywhere else

import "fmt"
import "runtime"

//import "sync"

func Util_testAddrIDMask() {
	for idx := 0; idx < 16; idx++ {
		var orig NodeID
		orig[8] = 42
		for bidx := 0; bidx < idx; bidx++ {
			orig[bidx/8] |= (0x80 >> uint8(bidx%8))
		}
		addr := address_addrForNodeID(&orig)
		nid, mask := addr.getNodeIDandMask()
		for b := 0; b < len(mask); b++ {
			nid[b] &= mask[b]
			orig[b] &= mask[b]
		}
		if *nid != orig {
			fmt.Println(orig)
			fmt.Println(*addr)
			fmt.Println(*nid)
			fmt.Println(*mask)
			panic(idx)
		}
	}
}

func util_yield() {
	runtime.Gosched()
}

func util_lockthread() {
	runtime.LockOSThread()
}

func util_unlockthread() {
	runtime.UnlockOSThread()
}

/*
var byteStore sync.Pool = sync.Pool{
  New: func () interface{} { return []byte(nil) },
}

func util_getBytes() []byte {
  return byteStore.Get().([]byte)[:0]
}

func util_putBytes(bs []byte) {
  byteStore.Put(bs) // FIXME? The cast to interface{} allocates...
}
*/

var byteStore chan []byte

func util_initByteStore() {
	if byteStore == nil {
		byteStore = make(chan []byte, 32)
	}
}

func util_getBytes() []byte {
	select {
	case bs := <-byteStore:
		return bs[:0]
	default:
		return nil
	}
}

func util_putBytes(bs []byte) {
	select {
	case byteStore <- bs:
	default:
	}
}
