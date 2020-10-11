package yggdrasil

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/types"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"

	"github.com/Arceliar/phony"
)

type MTU = types.MTU

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

// The Conn struct is a reference to an active connection session between the
// local node and a remote node. Conn implements the io.ReadWriteCloser
// interface and is used to send and receive traffic with a remote node.
type Conn struct {
	phony.Inbox
	core          *Core
	readDeadline  *time.Time
	writeDeadline *time.Time
	nodeID        *crypto.NodeID
	nodeMask      *crypto.NodeID
	session       *sessionInfo
	mtu           MTU
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

// String returns a string that uniquely identifies a connection. Currently this
// takes a form similar to "conn=0x0000000", which contains a memory reference
// to the Conn object. While this value should always be unique for each Conn
// object, the format of this is not strictly defined and may change in the
// future.
func (c *Conn) String() string {
	var s string
	phony.Block(c, func() { s = fmt.Sprintf("conn=%p", c) })
	return s
}

func (c *Conn) setMTU(from phony.Actor, mtu MTU) {
	c.Act(from, func() { c.mtu = mtu })
}

// This should never be called from an actor, used in the dial functions
func (c *Conn) search() error {
	var err error
	done := make(chan struct{})
	phony.Block(&c.core.router, func() {
		_, isIn := c.core.router.searches.searches[*c.nodeID]
		if !isIn {
			searchCompleted := func(sinfo *sessionInfo, e error) {
				select {
				case <-done:
					// Somehow this was called multiple times, TODO don't let that happen
					if sinfo != nil {
						// Need to clean up to avoid a session leak
						sinfo.cancel.Cancel(nil)
						sinfo.sessions.removeSession(sinfo)
					}
				default:
					if sinfo != nil {
						// Finish initializing the session
						c.session = sinfo
						c.session.setConn(nil, c)
						c.nodeID = crypto.GetNodeID(&c.session.theirPermPub)
						for i := range c.nodeMask {
							c.nodeMask[i] = 0xFF
						}
					}
					err = e
					close(done)
				}
			}
			sinfo := c.core.router.searches.newIterSearch(c.nodeID, c.nodeMask, searchCompleted)
			sinfo.startSearch()
		} else {
			err = errors.New("search already exists")
			close(done)
		}
	})
	<-done
	if c.session == nil && err == nil {
		panic("search failed but returned no error")
	}
	return err
}

// Used in session keep-alive traffic
func (c *Conn) _doSearch() {
	s := fmt.Sprintf("conn=%p", c)
	routerWork := func() {
		// Check to see if there is a search already matching the destination
		sinfo, isIn := c.core.router.searches.searches[*c.nodeID]
		if !isIn {
			// Nothing was found, so create a new search
			searchCompleted := func(sinfo *sessionInfo, e error) {}
			sinfo = c.core.router.searches.newIterSearch(c.nodeID, c.nodeMask, searchCompleted)
			c.core.log.Debugf("%s DHT search started: %p", s, sinfo)
			// Start the search
			sinfo.startSearch()
		}
	}
	c.core.router.Act(c.session, routerWork)
}

func (c *Conn) _getDeadlineCancellation(t *time.Time) (util.Cancellation, bool) {
	if t != nil {
		// A deadline is set, so return a Cancellation that uses it
		c := util.CancellationWithDeadline(c.session.cancel, *t)
		return c, true
	}
	// No deadline was set, so just return the existing cancellation and a dummy value
	return c.session.cancel, false
}

// SetReadCallback allows you to specify a function that will be called whenever
// a packet is received. This should be used if you wish to implement
// asynchronous patterns for receiving data from the remote node.
//
// Note that if a read callback has been supplied, you should no longer attempt
// to use the synchronous Read function.
func (c *Conn) SetReadCallback(callback func([]byte)) {
	c.Act(nil, func() {
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
		c.Act(nil, c._drainReadBuffer) // In case there's more
	default:
	}
}

// Called by the session to pass a new message to the Conn
func (c *Conn) recvMsg(from phony.Actor, msg []byte) {
	c.Act(from, func() {
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
func (c *Conn) readNoCopy() ([]byte, error) {
	var cancel util.Cancellation
	var doCancel bool
	phony.Block(c, func() { cancel, doCancel = c._getDeadlineCancellation(c.readDeadline) })
	if doCancel {
		defer cancel.Cancel(nil)
	}
	// Wait for some traffic to come through from the session
	select {
	case <-cancel.Finished():
		if cancel.Error() == util.CancellationTimeoutError {
			return nil, ConnError{errors.New("read timeout"), true, false, false, 0}
		}
		return nil, ConnError{errors.New("session closed"), false, false, true, 0}
	case bs := <-c.readBuffer:
		return bs, nil
	}
}

// Read allows you to read from the connection in a synchronous fashion. The
// function will block up until the point that either new data is available, the
// connection has been closed or the read deadline has been reached. If the
// function succeeds, the number of bytes read from the connection will be
// returned. Otherwise, an error condition will be returned.
//
// Note that you can also implement asynchronous reads by using SetReadCallback.
// If you do that, you should no longer attempt to use the Read function.
func (c *Conn) Read(b []byte) (int, error) {
	bs, err := c.readNoCopy()
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
	// Return the number of bytes copied to the slice, along with any error
	return n, err
}

func (c *Conn) _write(msg FlowKeyMessage) error {
	if len(msg.Message) > int(c.mtu) {
		return ConnError{errors.New("packet too big"), true, false, false, int(c.mtu)}
	}
	c.session.Act(c, func() {
		// Send the packet
		c.session._send(msg)
		// Session keep-alive, while we wait for the crypto workers from send
		switch {
		case time.Since(c.session.time) > 6*time.Second:
			if c.session.time.Before(c.session.pingTime) && time.Since(c.session.pingTime) > 6*time.Second {
				// TODO double check that the above condition is correct
				c._doSearch()
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

// WriteFrom should be called by a phony.Actor, and tells the Conn to send a
// message. This is used internally by Write. If the callback is called with a
// non-nil value, then it is safe to reuse the argument FlowKeyMessage.
func (c *Conn) WriteFrom(from phony.Actor, msg FlowKeyMessage, callback func(error)) {
	c.Act(from, func() {
		callback(c._write(msg))
	})
}

// writeNoCopy is used internally by Write and makes use of WriteFrom under the hood.
// The caller must not reuse the argument FlowKeyMessage when a nil error is returned.
func (c *Conn) writeNoCopy(msg FlowKeyMessage) error {
	var cancel util.Cancellation
	var doCancel bool
	phony.Block(c, func() { cancel, doCancel = c._getDeadlineCancellation(c.writeDeadline) })
	if doCancel {
		defer cancel.Cancel(nil)
	}
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

// Write allows you to write to the connection in a synchronous fashion. This
// function may block until either the write has completed, the connection has
// been closed or the write deadline has been reached. If the function succeeds,
// the number of written bytes is returned. Otherwise, an error condition is
// returned.
func (c *Conn) Write(b []byte) (int, error) {
	written := len(b)
	bs := make([]byte, 0, len(b)+crypto.BoxOverhead)
	bs = append(bs, b...)
	msg := FlowKeyMessage{Message: bs}
	err := c.writeNoCopy(msg)
	if err != nil {
		written = 0
	}
	return written, err
}

// Close will close an open connection and any blocking operations on the
// connection will unblock and return. From this point forward, the connection
// can no longer be used and you should no longer attempt to Read or Write to
// the connection.
func (c *Conn) Close() (err error) {
	phony.Block(c, func() {
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

// LocalAddr returns the complete public key of the local side of the
// connection. This is always going to return your own node's public key.
func (c *Conn) LocalAddr() net.Addr {
	return &c.core.boxPub
}

// RemoteAddr returns the complete public key of the remote side of the
// connection.
func (c *Conn) RemoteAddr() net.Addr {
	if c.session != nil {
		return &c.session.theirPermPub
	}
	return nil
}

// SetDeadline is equivalent to calling both SetReadDeadline and
// SetWriteDeadline with the same value, configuring the maximum amount of time
// that synchronous Read and Write operations can block for. If no deadline is
// configured, Read and Write operations can potentially block indefinitely.
func (c *Conn) SetDeadline(t time.Time) error {
	c.SetReadDeadline(t)
	c.SetWriteDeadline(t)
	return nil
}

// SetReadDeadline configures the maximum amount of time that a synchronous Read
// operation can block for. A Read operation will unblock at the point that the
// read deadline is reached if no other condition (such as data arrival or
// connection closure) happens first. If no deadline is configured, Read
// operations can potentially block indefinitely.
func (c *Conn) SetReadDeadline(t time.Time) error {
	// TODO warn that this can block while waiting for the Conn actor to run, so don't call it from other actors...
	phony.Block(c, func() { c.readDeadline = &t })
	return nil
}

// SetWriteDeadline configures the maximum amount of time that a synchronous
// Write operation can block for. A Write operation will unblock at the point
// that the read deadline is reached if no other condition (such as data sending
// or connection closure) happens first. If no deadline is configured, Write
// operations can potentially block indefinitely.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	// TODO warn that this can block while waiting for the Conn actor to run, so don't call it from other actors...
	phony.Block(c, func() { c.writeDeadline = &t })
	return nil
}
