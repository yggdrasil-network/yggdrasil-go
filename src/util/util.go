package util

// These are misc. utility functions that didn't really fit anywhere else

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A wrapper around runtime.Gosched() so it doesn't need to be imported elsewhere.
func Yield() {
	runtime.Gosched()
}

// A wrapper around runtime.LockOSThread() so it doesn't need to be imported elsewhere.
func LockThread() {
	runtime.LockOSThread()
}

// A wrapper around runtime.UnlockOSThread() so it doesn't need to be imported elsewhere.
func UnlockThread() {
	runtime.UnlockOSThread()
}

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

// Gets a slice of the appropriate length, reusing existing slice capacity when possible
func ResizeBytes(bs []byte, length int) []byte {
	if cap(bs) >= length {
		return bs[:length]
	} else {
		return make([]byte, length)
	}
}

// This is a workaround to go's broken timer implementation
func TimerStop(t *time.Timer) bool {
	stopped := t.Stop()
	select {
	case <-t.C:
	default:
	}
	return stopped
}

// Run a blocking function with a timeout.
// Returns true if the function returns.
// Returns false if the timer fires.
// The blocked function remains blocked--the caller is responsible for somehow killing it.
func FuncTimeout(f func(), timeout time.Duration) bool {
	success := make(chan struct{})
	go func() {
		defer close(success)
		f()
	}()
	timer := time.NewTimer(timeout)
	defer TimerStop(timer)
	select {
	case <-success:
		return true
	case <-timer.C:
		return false
	}
}

// This calculates the difference between two arrays and returns items
// that appear in A but not in B - useful somewhat when reconfiguring
// and working out what configuration items changed
func Difference(a, b []string) []string {
	ab := []string{}
	mb := map[string]bool{}
	for _, x := range b {
		mb[x] = true
	}
	for _, x := range a {
		if !mb[x] {
			ab = append(ab, x)
		}
	}
	return ab
}

// DecodeCoordString decodes a string representing coordinates in [1 2 3] format
// and returns a []uint64.
func DecodeCoordString(in string) (out []uint64) {
	s := strings.Trim(in, "[]")
	t := strings.Split(s, " ")
	for _, a := range t {
		if u, err := strconv.ParseUint(a, 0, 64); err == nil {
			out = append(out, u)
		}
	}
	return out
}

// GetFlowLabel takes an IP packet as an argument and returns some information about the traffic flow.
// For IPv4 packets, this is derived from the source and destination protocol and port numbers.
// For IPv6 packets, this is derived from the FlowLabel field of the packet if this was set, otherwise it's handled like IPv4.
// The FlowKey is then used internally by Yggdrasil for congestion control.
func GetFlowKey(bs []byte) uint64 {
	// Work out the flowkey - this is used to determine which switch queue
	// traffic will be pushed to in the event of congestion
	var flowkey uint64
	// Get the IP protocol version from the packet
	switch bs[0] & 0xf0 {
	case 0x40: // IPv4 packet
		// Check the packet meets minimum UDP packet length
		if len(bs) >= 24 {
			// Is the protocol TCP, UDP or SCTP?
			if bs[9] == 0x06 || bs[9] == 0x11 || bs[9] == 0x84 {
				ihl := bs[0] & 0x0f * 4 // Header length
				flowkey = uint64(bs[9])<<32 /* proto */ |
					uint64(bs[ihl+0])<<24 | uint64(bs[ihl+1])<<16 /* sport */ |
					uint64(bs[ihl+2])<<8 | uint64(bs[ihl+3]) /* dport */
			}
		}
	case 0x60: // IPv6 packet
		// Check if the flowlabel was specified in the packet header
		flowkey = uint64(bs[1]&0x0f)<<16 | uint64(bs[2])<<8 | uint64(bs[3])
		// If the flowlabel isn't present, make protokey from proto | sport | dport
		// if the packet meets minimum UDP packet length
		if flowkey == 0 && len(bs) >= 48 {
			// Is the protocol TCP, UDP or SCTP?
			if bs[6] == 0x06 || bs[6] == 0x11 || bs[6] == 0x84 {
				flowkey = uint64(bs[6])<<32 /* proto */ |
					uint64(bs[40])<<24 | uint64(bs[41])<<16 /* sport */ |
					uint64(bs[42])<<8 | uint64(bs[43]) /* dport */
			}
		}
	}
	return flowkey
}
