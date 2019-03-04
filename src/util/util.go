package util

// These are misc. utility functions that didn't really fit anywhere else

import "runtime"
import "time"

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
// It's used like a sync.Pool, but with a fixed size and typechecked without type casts to/from interface{} (which were making the profiles look ugly).
var byteStore chan []byte

func init() {
	byteStore = make(chan []byte, 32)
}

// Gets an empty slice from the byte store, if one is available, or else returns a new nil slice.
func GetBytes() []byte {
	select {
	case bs := <-byteStore:
		return bs[:0]
	default:
		return nil
	}
}

// Puts a slice in the store, if there's room, or else returns and lets the slice get collected.
func PutBytes(bs []byte) {
	select {
	case byteStore <- bs:
	default:
	}
}

// This is a workaround to go's broken timer implementation
func TimerStop(t *time.Timer) bool {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	return true
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
		if _, ok := mb[x]; !ok {
			ab = append(ab, x)
		}
	}
	return ab
}
