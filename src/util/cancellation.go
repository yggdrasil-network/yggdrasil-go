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

var CancellationFinalized = errors.New("finalizer called")
var CancellationTimeoutError = errors.New("timeout")

func CancellationFinalizer(c Cancellation) {
	c.Cancel(CancellationFinalized)
}

type cancellation struct {
	cancel chan struct{}
	mutex  sync.RWMutex
	err    error
	done   bool
}

func NewCancellation() Cancellation {
	c := cancellation{
		cancel: make(chan struct{}),
	}
	runtime.SetFinalizer(&c, CancellationFinalizer)
	return &c
}

func (c *cancellation) Finished() <-chan struct{} {
	return c.cancel
}

func (c *cancellation) Cancel(err error) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.done {
		return c.err
	} else {
		c.err = err
		c.done = true
		close(c.cancel)
		return nil
	}
}

func (c *cancellation) Error() error {
	c.mutex.RLock()
	err := c.err
	c.mutex.RUnlock()
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
			child.Cancel(CancellationTimeoutError)
		}
	}()
	return child
}

func CancellationWithDeadline(parent Cancellation, deadline time.Time) Cancellation {
	return CancellationWithTimeout(parent, deadline.Sub(time.Now()))
}
