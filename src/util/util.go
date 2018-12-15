package yggdrasil

// These are misc. utility functions that didn't really fit anywhere else

import "runtime"

// A wrapper around runtime.Gosched() so it doesn't need to be imported elsewhere.
func util_yield() {
	runtime.Gosched()
}

// A wrapper around runtime.LockOSThread() so it doesn't need to be imported elsewhere.
func util_lockthread() {
	runtime.LockOSThread()
}

// A wrapper around runtime.UnlockOSThread() so it doesn't need to be imported elsewhere.
func util_unlockthread() {
	runtime.UnlockOSThread()
}

// This is used to buffer recently used slices of bytes, to prevent allocations in the hot loops.
// It's used like a sync.Pool, but with a fixed size and typechecked without type casts to/from interface{} (which were making the profiles look ugly).
var byteStore chan []byte

// Initializes the byteStore
func util_initByteStore() {
	if byteStore == nil {
		byteStore = make(chan []byte, 32)
	}
}

// Gets an empty slice from the byte store, if one is available, or else returns a new nil slice.
func util_getBytes() []byte {
	select {
	case bs := <-byteStore:
		return bs[:0]
	default:
		return nil
	}
}

// Puts a slice in the store, if there's room, or else returns and lets the slice get collected.
func util_putBytes(bs []byte) {
	select {
	case byteStore <- bs:
	default:
	}
}
