//+build mobile

package util

import "runtime/debug"

func init() {
	debug.SetGCPercent(25)
}

// GetBytes always returns a nil slice on mobile platforms.
func GetBytes() []byte {
	return nil
}

// PutBytes does literally nothing on mobile platforms.
// This is done rather than keeping a free list of bytes on platforms with memory constraints.
// It's needed to help keep memory usage low enough to fall under the limits set for e.g. iOS NEPacketTunnelProvider apps.
func PutBytes(bs []byte) {
	return
}
