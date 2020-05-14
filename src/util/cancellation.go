package util

import (
	"errors"
	"runtime"
	"sync"
	"time"
)

// Cancellation is used to signal when things should shut down, such as signaling anything associated with a Conn to exit.
// This is and is similar to a context, but with an error to specify the reason for the cancellation.
type Cancellation interface {
	Finished() <-chan struct{} // Finished returns a channel which will be closed when Cancellation.Cancel is first called.
	Cancel(error) error        // Cancel closes the channel returned by Finished and sets the error returned by error, or else returns the existing error if the Cancellation has already run.
	Error() error              // Error returns the error provided to Cancel, or nil if no error has been provided.
}

// CancellationFinalized is an error returned if a cancellation object was garbage collected and the finalizer was run.
// If you ever see this, then you're probably doing something wrong with your code.
var CancellationFinalized = errors.New("finalizer called")

// CancellationTimeoutError is used when a CancellationWithTimeout or CancellationWithDeadline is cancelled due to said timeout.
var CancellationTimeoutError = errors.New("timeout")

// CancellationFinalizer is set as a finalizer when creating a new cancellation with NewCancellation(), and generally shouldn't be needed by the user, but is included in case other implementations of the same interface want to make use of it.
func CancellationFinalizer(c Cancellation) {
	c.Cancel(CancellationFinalized)
}

type cancellation struct {
	cancel chan struct{}
	mutex  sync.RWMutex
	err    error
	done   bool
}

// NewCancellation returns a pointer to a struct satisfying the Cancellation interface.
func NewCancellation() Cancellation {
	c := cancellation{
		cancel: make(chan struct{}),
	}
	runtime.SetFinalizer(&c, CancellationFinalizer)
	return &c
}

// Finished returns a channel which will be closed when Cancellation.Cancel is first called.
func (c *cancellation) Finished() <-chan struct{} {
	return c.cancel
}

// Cancel closes the channel returned by Finished and sets the error returned by error, or else returns the existing error if the Cancellation has already run.
func (c *cancellation) Cancel(err error) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.done {
		return c.err
	}
	c.err = err
	c.done = true
	close(c.cancel)
	return nil
}

// Error returns the error provided to Cancel, or nil if no error has been provided.
func (c *cancellation) Error() error {
	c.mutex.RLock()
	err := c.err
	c.mutex.RUnlock()
	return err
}

// CancellationChild returns a new Cancellation which can be Cancelled independently of the parent, but which will also be Cancelled if the parent is Cancelled first.
func CancellationChild(parent Cancellation) Cancellation {
	child := NewCancellation()
	go func() {
		select {
		case <-child.Finished():
		case <-parent.Finished():
			child.Cancel(parent.Error())
		}
	}()
	return child
}

// CancellationWithTimeout returns a ChildCancellation that will automatically be Cancelled with a CancellationTimeoutError after the timeout.
func CancellationWithTimeout(parent Cancellation, timeout time.Duration) Cancellation {
	child := CancellationChild(parent)
	go func() {
		timer := time.NewTimer(timeout)
		defer TimerStop(timer)
		select {
		case <-child.Finished():
		case <-timer.C:
			child.Cancel(CancellationTimeoutError)
		}
	}()
	return child
}

// CancellationWithTimeout returns a ChildCancellation that will automatically be Cancelled with a CancellationTimeoutError after the specified deadline.
func CancellationWithDeadline(parent Cancellation, deadline time.Time) Cancellation {
	return CancellationWithTimeout(parent, deadline.Sub(time.Now()))
}
