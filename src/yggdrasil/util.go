package yggdrasil

// These are misc. utility functions that didn't really fit anywhere else

import "runtime"

//import "sync"

func util_yield() {
	runtime.Gosched()
}

func util_lockthread() {
	runtime.LockOSThread()
}

func util_unlockthread() {
	runtime.UnlockOSThread()
}

/* Used previously, but removed because casting to an interface{} allocates...
var byteStore sync.Pool = sync.Pool{
  New: func () interface{} { return []byte(nil) },
}

func util_getBytes() []byte {
  return byteStore.Get().([]byte)[:0]
}

func util_putBytes(bs []byte) {
  byteStore.Put(bs) // This is the part that allocates
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
