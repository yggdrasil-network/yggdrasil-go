// Package util contains miscellaneous utilities used by yggdrasil.
// In particular, this includes a crypto worker pool, Cancellation machinery, and a sync.Pool used to reuse []byte.
package util

// These are misc. utility functions that didn't really fit anywhere else

import (
	"time"
)

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
func FuncTimeout(timeout time.Duration, f func()) bool {
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
