//+build !mobile

package util

import "sync"

// This is used to buffer recently used slices of bytes, to prevent allocations in the hot loops.
var byteStore = sync.Pool{New: func() interface{} { return []byte(nil) }}

// GetBytes returns a 0-length (possibly nil) slice of bytes from a free list, so it may have a larger capacity.
func GetBytes() []byte {
	return byteStore.Get().([]byte)[:0]
}

// PutBytes stores a slice in a free list, where it can potentially be reused to prevent future allocations.
func PutBytes(bs []byte) {
	byteStore.Put(bs)
}
