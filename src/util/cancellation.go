package util

import (
	"errors"
	"runtime"
	"sync"
	"time"
)

type Cancellation interface {
	Finished() <-chan struct{}
	Cancel(error) error
	Error() error
}

func CancellationFinalizer(c Cancellation) {
	c.Cancel(errors.New("finalizer called"))
}

type cancellation struct {
	signal chan error
	cancel chan struct{}
	errMtx sync.RWMutex
	err    error
}

func (c *cancellation) worker() {
	// Launch this in a separate goroutine when creating a cancellation
	err := <-c.signal
	c.errMtx.Lock()
	c.err = err
	c.errMtx.Unlock()
	close(c.cancel)
}

func NewCancellation() Cancellation {
	c := cancellation{
		signal: make(chan error),
		cancel: make(chan struct{}),
	}
	runtime.SetFinalizer(&c, CancellationFinalizer)
	go c.worker()
	return &c
}

func (c *cancellation) Finished() <-chan struct{} {
	return c.cancel
}

func (c *cancellation) Cancel(err error) error {
	select {
	case c.signal <- err:
		return nil
	case <-c.cancel:
		return c.Error()
	}
}

func (c *cancellation) Error() error {
	c.errMtx.RLock()
	err := c.err
	c.errMtx.RUnlock()
	return err
}

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

func CancellationWithTimeout(parent Cancellation, timeout time.Duration) Cancellation {
	child := CancellationChild(parent)
	go func() {
		timer := time.NewTimer(timeout)
		defer TimerStop(timer)
		select {
		case <-child.Finished():
		case <-timer.C:
			child.Cancel(errors.New("timeout"))
		}
	}()
	return child
}

func CancellationWithDeadline(parent Cancellation, deadline time.Time) Cancellation {
	return CancellationWithTimeout(parent, deadline.Sub(time.Now()))
}
