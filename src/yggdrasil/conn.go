package yggdrasil

import (
	"errors"
	"fmt"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"

	"github.com/Arceliar/phony"
)

// ConnError implements the net.Error interface
type ConnError struct {
	error
	timeout   bool
	temporary bool
	closed    bool
	maxsize   int
}

// Timeout returns true if the error relates to a timeout condition on the
// connection.
func (e *ConnError) Timeout() bool {
	return e.timeout
}

// Temporary return true if the error is temporary or false if it is a permanent
// error condition.
func (e *ConnError) Temporary() bool {
	return e.temporary
}

// PacketTooBig returns in response to sending a packet that is too large, and
// if so, the maximum supported packet size that should be used for the
// connection.
func (e *ConnError) PacketTooBig() bool {
	return e.maxsize > 0
}

// PacketMaximumSize returns the maximum supported packet size. This will only
// return a non-zero value if ConnError.PacketTooBig() returns true.
func (e *ConnError) PacketMaximumSize() int {
	if !e.PacketTooBig() {
		return 0
	}
	return e.maxsize
}

// Closed returns if the session is already closed and is now unusable.
func (e *ConnError) Closed() bool {
	return e.closed
}

type Conn struct {
	phony.Inbox
	core          *Core
	readDeadline  *time.Time
	writeDeadline *time.Time
	nodeID        *crypto.NodeID
	nodeMask      *crypto.NodeID
	session       *sessionInfo
	mtu           uint16
	readCallback  func([]byte)
	readBuffer    chan []byte
}

// TODO func NewConn() that initializes additional fields as needed
func newConn(core *Core, nodeID *crypto.NodeID, nodeMask *crypto.NodeID, session *sessionInfo) *Conn {
	conn := Conn{
		core:       core,
		nodeID:     nodeID,
		nodeMask:   nodeMask,
		session:    session,
		readBuffer: make(chan []byte, 1024),
	}
	return &conn
}

func (c *Conn) String() string {
	var s string
	<-c.SyncExec(func() { s = fmt.Sprintf("conn=%p", c) })
	return s
}

func (c *Conn) setMTU(from phony.Actor, mtu uint16) {
	c.RecvFrom(from, func() { c.mtu = mtu })
}

// This should never be called from the router goroutine, used in the dial functions
func (c *Conn) search() error {
	var sinfo *searchInfo
	var isIn bool
	c.core.router.doAdmin(func() { sinfo, isIn = c.core.router.searches.searches[*c.nodeID] })
	if !isIn {
		done := make(chan struct{}, 1)
		var sess *sessionInfo
		var err error
		searchCompleted := func(sinfo *sessionInfo, e error) {
			sess = sinfo
			err = e
			// FIXME close can be called multiple times, do a non-blocking send instead
			select {
			case done <- struct{}{}:
			default:
			}
		}
		c.core.router.doAdmin(func() {
			sinfo = c.core.router.searches.newIterSearch(c.nodeID, c.nodeMask, searchCompleted)
			sinfo.continueSearch()
		})
		<-done
		c.session = sess
		if c.session == nil && err == nil {
			panic("search failed but returned no error")
		}
		if c.session != nil {
			c.nodeID = crypto.GetNodeID(&c.session.theirPermPub)
			for i := range c.nodeMask {
				c.nodeMask[i] = 0xFF
			}
			c.session.conn = c
		}
		return err
	} else {
		return errors.New("search already exists")
	}
	return nil
}

// Used in session keep-alive traffic
func (c *Conn) doSearch() {
	routerWork := func() {
		// Check to see if there is a search already matching the destination
		sinfo, isIn := c.core.router.searches.searches[*c.nodeID]
		if !isIn {
			// Nothing was found, so create a new search
			searchCompleted := func(sinfo *sessionInfo, e error) {}
			sinfo = c.core.router.searches.newIterSearch(c.nodeID, c.nodeMask, searchCompleted)
			c.core.log.Debugf("%s DHT search started: %p", c.String(), sinfo)
			// Start the search
			sinfo.continueSearch()
		}
	}
	c.core.router.RecvFrom(c.session, routerWork)
}

func (c *Conn) _getDeadlineCancellation(t *time.Time) (util.Cancellation, bool) {
	if t != nil {
		// A deadline is set, so return a Cancellation that uses it
		c := util.CancellationWithDeadline(c.session.cancel, *t)
		return c, true
	} else {
		// No deadline was set, so just return the existinc cancellation and a dummy value
		return c.session.cancel, false
	}
}

// SetReadCallback sets a callback which will be called whenever a packet is received.
func (c *Conn) SetReadCallback(callback func([]byte)) {
	c.RecvFrom(nil, func() {
		c.readCallback = callback
		c._drainReadBuffer()
	})
}

func (c *Conn) _drainReadBuffer() {
	if c.readCallback == nil {
		return
	}
	select {
	case bs := <-c.readBuffer:
		c.readCallback(bs)
		c.RecvFrom(nil, c._drainReadBuffer) // In case there's more
	default:
	}
}

// Called by the session to pass a new message to the Conn
func (c *Conn) recvMsg(from phony.Actor, msg []byte) {
	c.RecvFrom(from, func() {
		if c.readCallback != nil {
			c.readCallback(msg)
		} else {
			select {
			case c.readBuffer <- msg:
			default:
			}
		}
	})
}

// Used internally by Read, the caller is responsible for util.PutBytes when they're done.
func (c *Conn) ReadNoCopy() ([]byte, error) {
	var cancel util.Cancellation
	var doCancel bool
	<-c.SyncExec(func() { cancel, doCancel = c._getDeadlineCancellation(c.readDeadline) })
	if doCancel {
		defer cancel.Cancel(nil)
	}
	// Wait for some traffic to come through from the session
	select {
	case <-cancel.Finished():
		if cancel.Error() == util.CancellationTimeoutError {
			return nil, ConnError{errors.New("read timeout"), true, false, false, 0}
		} else {
			return nil, ConnError{errors.New("session closed"), false, false, true, 0}
		}
	case bs := <-c.readBuffer:
		return bs, nil
	}
}

// Implements net.Conn.Read
func (c *Conn) Read(b []byte) (int, error) {
	bs, err := c.ReadNoCopy()
	if err != nil {
		return 0, err
	}
	n := len(bs)
	if len(bs) > len(b) {
		n = len(b)
		err = ConnError{errors.New("read buffer too small for entire packet"), false, true, false, 0}
	}
	// Copy results to the output slice and clean up
	copy(b, bs)
	util.PutBytes(bs)
	// Return the number of bytes copied to the slice, along with any error
	return n, err
}

func (c *Conn) _write(msg FlowKeyMessage) error {
	if len(msg.Message) > int(c.mtu) {
		return ConnError{errors.New("packet too big"), true, false, false, int(c.mtu)}
	}
	c.session.RecvFrom(c, func() {
		// Send the packet
		c.session._send(msg)
		// Session keep-alive, while we wait for the crypto workers from send
		switch {
		case time.Since(c.session.time) > 6*time.Second:
			if c.session.time.Before(c.session.pingTime) && time.Since(c.session.pingTime) > 6*time.Second {
				// TODO double check that the above condition is correct
				c.doSearch()
			} else {
				c.session.ping(c.session) // TODO send from self if this becomes an actor
			}
		case c.session.reset && c.session.pingTime.Before(c.session.time):
			c.session.ping(c.session) // TODO send from self if this becomes an actor
		default: // Don't do anything, to keep traffic throttled
		}
	})
	return nil
}

// WriteFrom should be called by a phony.Actor, and tells the Conn to send a message.
// This is used internaly by WriteNoCopy and Write.
// If the callback is called with a non-nil value, then it is safe to reuse the argument FlowKeyMessage.
func (c *Conn) WriteFrom(from phony.Actor, msg FlowKeyMessage, callback func(error)) {
	c.RecvFrom(from, func() {
		callback(c._write(msg))
	})
}

// WriteNoCopy is used internally by Write and makes use of WriteFrom under the hood.
// The caller must not reuse the argument FlowKeyMessage when a nil error is returned.
func (c *Conn) WriteNoCopy(msg FlowKeyMessage) error {
	var cancel util.Cancellation
	var doCancel bool
	<-c.SyncExec(func() { cancel, doCancel = c._getDeadlineCancellation(c.writeDeadline) })
	var err error
	select {
	case <-cancel.Finished():
		if cancel.Error() == util.CancellationTimeoutError {
			err = ConnError{errors.New("write timeout"), true, false, false, 0}
		} else {
			err = ConnError{errors.New("session closed"), false, false, true, 0}
		}
	default:
		done := make(chan struct{})
		callback := func(e error) { err = e; close(done) }
		c.WriteFrom(nil, msg, callback)
		<-done
	}
	return err
}

// Write implement the Write function of a net.Conn, and makes use of WriteNoCopy under the hood.
func (c *Conn) Write(b []byte) (int, error) {
	written := len(b)
	msg := FlowKeyMessage{Message: append(util.GetBytes(), b...)}
	err := c.WriteNoCopy(msg)
	if err != nil {
		util.PutBytes(msg.Message)
		written = 0
	}
	return written, err
}

func (c *Conn) Close() (err error) {
	<-c.SyncExec(func() {
		if c.session != nil {
			// Close the session, if it hasn't been closed already
			if e := c.session.cancel.Cancel(errors.New("connection closed")); e != nil {
				err = ConnError{errors.New("close failed, session already closed"), false, false, true, 0}
			} else {
				c.session.doRemove()
			}
		}
	})
	return
}

func (c *Conn) LocalAddr() crypto.NodeID {
	return *crypto.GetNodeID(&c.core.boxPub)
}

func (c *Conn) RemoteAddr() crypto.NodeID {
	// TODO warn that this can block while waiting for the Conn actor to run, so don't call it from other actors...
	var n crypto.NodeID
	<-c.SyncExec(func() { n = *c.nodeID })
	return n
}

func (c *Conn) SetDeadline(t time.Time) error {
	c.SetReadDeadline(t)
	c.SetWriteDeadline(t)
	return nil
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	// TODO warn that this can block while waiting for the Conn actor to run, so don't call it from other actors...
	<-c.SyncExec(func() { c.readDeadline = &t })
	return nil
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	// TODO warn that this can block while waiting for the Conn actor to run, so don't call it from other actors...
	<-c.SyncExec(func() { c.writeDeadline = &t })
	return nil
}
