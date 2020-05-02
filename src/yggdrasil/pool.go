package yggdrasil

import "sync"

// Used internally to reduce allocations in the hot loop
//  I.e. packets being switched or between the crypto and the switch
// For safety reasons, these must not escape this package
var pool = sync.Pool{New: func() interface{} { return []byte(nil) }}

func pool_getBytes(size int) []byte {
	bs := pool.Get().([]byte)
	if cap(bs) < size {
		bs = make([]byte, size)
	}
	return bs[:size]
}

func pool_putBytes(bs []byte) {
	pool.Put(bs)
}
