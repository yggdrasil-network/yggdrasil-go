package core

import "sync"

var bytePool = sync.Pool{New: func() interface{} { return []byte(nil) }}

func allocBytes(size int) []byte {
	bs := bytePool.Get().([]byte)
	if cap(bs) < size {
		bs = make([]byte, size)
	}
	return bs[:size]
}

func freeBytes(bs []byte) {
	bytePool.Put(bs[:0]) //nolint:staticcheck
}
