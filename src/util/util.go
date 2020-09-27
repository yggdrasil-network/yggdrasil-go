// Package util contains miscellaneous utilities used by yggdrasil.
// In particular, this includes a crypto worker pool, Cancellation machinery, and a sync.Pool used to reuse []byte.
package util

// These are misc. utility functions that didn't really fit anywhere else

import (
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Yield just executes runtime.Gosched(), and is included so we don't need to explicitly import runtime elsewhere.
func Yield() {
	runtime.Gosched()
}

// LockThread executes runtime.LockOSThread(), and is included so we don't need to explicitly import runtime elsewhere.
func LockThread() {
	runtime.LockOSThread()
}

// UnlockThread executes runtime.UnlockOSThread(), and is included so we don't need to explicitly import runtime elsewhere.
func UnlockThread() {
	runtime.UnlockOSThread()
}

// ResizeBytes returns a slice of the specified length. If the provided slice has sufficient capacity, it will be resized and returned rather than allocating a new slice.
func ResizeBytes(bs []byte, length int) []byte {
	if cap(bs) >= length {
		return bs[:length]
	}
	return make([]byte, length)
}

// TimerStop stops a timer and makes sure the channel is drained, returns true if the timer was stopped before firing.
func TimerStop(t *time.Timer) bool {
	stopped := t.Stop()
	select {
	case <-t.C:
	default:
	}
	return stopped
}

// FuncTimeout runs the provided function in a separate goroutine, and returns true if the function finishes executing before the timeout passes, or false if the timeout passes.
// It includes no mechanism to stop the function if the timeout fires, so the user is expected to do so on their own (such as with a Cancellation or a context).
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

// Difference loops over two strings and returns the elements of A which do not appear in B.
// This is somewhat useful when needing to determine which elements of a configuration file have changed.
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

// GetFlowKey takes an IP packet as an argument and returns some information about the traffic flow.
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
		ihl := (bs[0] & 0x0f) * 4 // whole IPv4 header length (min 20)
		// 8 is minimum UDP packet length
		if ihl >= 20 && len(bs)-int(ihl) >= 8 {
			switch bs[9] /* protocol */ {
			case 0x06 /* TCP */, 0x11 /* UDP */, 0x84 /* SCTP */ :
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
			switch bs[9] /* protocol */ {
			case 0x06 /* TCP */, 0x11 /* UDP */, 0x84 /* SCTP */ :
				flowkey = uint64(bs[6])<<32 /* proto */ |
					uint64(bs[40])<<24 | uint64(bs[41])<<16 /* sport */ |
					uint64(bs[42])<<8 | uint64(bs[43]) /* dport */
			}
		}
	}
	return flowkey
}
