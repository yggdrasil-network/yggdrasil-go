//+build mobile

package util

import "runtime/debug"

func init() {
	debug.SetGCPercent(25)
}

// On mobile, just return a nil slice.
func GetBytes() []byte {
	return nil
}

// On mobile, don't do anything.
func PutBytes(bs []byte) {
	return
}
