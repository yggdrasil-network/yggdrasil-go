package yggdrasil

import (
	"errors"
	"net"
)

// Listener waits for incoming sessions
type Listener struct {
	conn  chan *Conn
	close chan interface{}
}

// Accept blocks until a new incoming session is received
func (l *Listener) Accept() (*Conn, error) {
	select {
	case c, ok := <-l.conn:
		if !ok {
			return nil, errors.New("listener closed")
		}
		return c, nil
	case <-l.close:
		return nil, errors.New("listener closed")
	}
}

// Close will stop the listener
func (l *Listener) Close() (err error) {
	defer func() {
		recover()
		err = errors.New("already closed")
	}()
	close(l.close)
	close(l.conn)
	return nil
}

// Addr is not implemented for this type yet
func (l *Listener) Addr() net.Addr {
	return nil
}
