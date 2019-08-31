//+build !mobile

package util

import "sync"

// This is used to buffer recently used slices of bytes, to prevent allocations in the hot loops.
var byteStore = sync.Pool{New: func() interface{} { return []byte(nil) }}

// Gets an empty slice from the byte store.
func GetBytes() []byte {
	return byteStore.Get().([]byte)[:0]
}

// Puts a slice in the store.
func PutBytes(bs []byte) {
	byteStore.Put(bs)
}
