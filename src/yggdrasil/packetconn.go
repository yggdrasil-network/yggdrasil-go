package yggdrasil

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/Arceliar/phony"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/types"
)

type packet struct {
	addr    net.Addr
	payload []byte
}

type PacketConn struct {
	phony.Inbox
	net.PacketConn
	sessions      *sessions
	closed        bool
	readCallback  func(net.Addr, []byte)
	readBuffer    chan packet
	readDeadline  *time.Time
	writeDeadline *time.Time
}

func newPacketConn(ss *sessions) *PacketConn {
	return &PacketConn{
		sessions:   ss,
		readBuffer: make(chan packet),
	}
}

func (c *PacketConn) _sendToReader(addr net.Addr, payload []byte) {
	if c.readCallback == nil {
		c.readBuffer <- packet{
			addr:    addr,
			payload: payload,
		}
	} else {
		c.readCallback(addr, payload)
	}
}

// implements net.PacketConn
func (c *PacketConn) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	if c.readCallback != nil {
		return 0, nil, errors.New("read callback is configured")
	}
	if c.closed { // TODO: unsafe?
		return 0, nil, PacketConnError{closed: true}
	}
	packet := <-c.readBuffer
	copy(b, packet.payload)
	return len(packet.payload), packet.addr, nil
}

// implements net.PacketConn
func (c *PacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if c.closed { // TODO: unsafe?
		return 0, PacketConnError{closed: true}
	}

	boxPubKey, ok := addr.(*crypto.BoxPubKey)
	if !ok {
		return 0, errors.New("expected *crypto.BoxPubKey as net.Addr")
	}
	nodeID := crypto.GetNodeID(boxPubKey)
	nodeMask := &crypto.NodeID{}
	for i := range nodeMask {
		nodeMask[i] = 0xFF
	}

	// TODO: This is all a mess
	var err error
	var session *sessionInfo
	phony.Block(c.sessions.router, func() {
		session, ok = c.sessions.getByTheirPerm(boxPubKey)
	})
	if !ok {
		nodeID, boxPubKey, err = c.sessions.router.core.Resolve(nodeID, nodeMask)
		if err == nil {
			phony.Block(c.sessions.router, func() {
				session, ok = c.sessions.getByTheirPerm(boxPubKey)
			})
		}
	}
	if err != nil {
		return 0, fmt.Errorf("failed to find session/start search: %w", err)
	}
	if !ok || session == nil {
		return 0, errors.New("expected a session but there was none")
	}

	sendErr := make(chan error, 1)
	msg := FlowKeyMessage{Message: b}

	session.Act(c, func() {
		// Check if the packet is small enough to go through this session
		sessionMTU := session._getMTU()
		if types.MTU(len(b)) > sessionMTU {
			sendErr <- PacketConnError{maxsize: int(sessionMTU)}
			return
		}

		// Send the packet
		session._send(msg)

		// Session keep-alive, while we wait for the crypto workers from send
		switch {
		case time.Since(session.time) > 6*time.Second:
			if session.time.Before(session.pingTime) && time.Since(session.pingTime) > 6*time.Second {
				// TODO double check that the above condition is correct
				c.sessions.router.Act(session, func() {
					// Check to see if there is a search already matching the destination
					sinfo, isIn := c.sessions.router.searches.searches[*nodeID]
					if !isIn {
						// Nothing was found, so create a new search
						searchCompleted := func(sinfo *sessionInfo, e error) {}
						sinfo = c.sessions.router.searches.newIterSearch(nodeID, nodeMask, searchCompleted)
						c.sessions.router.core.log.Debugf("DHT search started: %p", sinfo)
						// Start the search
						sinfo.startSearch()
					}
				})
			} else {
				session.ping(session) // TODO send from self if this becomes an actor
			}
		case session.reset && session.pingTime.Before(session.time):
			session.ping(session) // TODO send from self if this becomes an actor
		default: // Don't do anything, to keep traffic throttled
		}

		sendErr <- nil
	})

	err = <-sendErr
	return len(b), err
}

// implements net.PacketConn
func (c *PacketConn) Close() error {
	// TODO: implement this. don't know what makes sense for net.PacketConn?
	return nil
}

// implements net.PacketConn
func (c *PacketConn) LocalAddr() net.Addr {
	return &c.sessions.router.core.boxPub
}

// SetReadCallback allows you to specify a function that will be called whenever
// a packet is received. This should be used if you wish to implement
// asynchronous patterns for receiving data from the remote node.
//
// Note that if a read callback has been supplied, you should no longer attempt
// to use the synchronous Read function.
func (c *PacketConn) SetReadCallback(callback func(net.Addr, []byte)) {
	c.Act(nil, func() {
		c.readCallback = callback
		c._drainReadBuffer()
	})
}

func (c *PacketConn) _drainReadBuffer() {
	if c.readCallback == nil {
		return
	}
	select {
	case bs := <-c.readBuffer:
		c.readCallback(bs.addr, bs.payload)
		c.Act(nil, c._drainReadBuffer) // In case there's more
	default:
	}
}

// SetDeadline is equivalent to calling both SetReadDeadline and
// SetWriteDeadline with the same value, configuring the maximum amount of time
// that synchronous Read and Write operations can block for. If no deadline is
// configured, Read and Write operations can potentially block indefinitely.
func (c *PacketConn) SetDeadline(t time.Time) error {
	c.SetReadDeadline(t)
	c.SetWriteDeadline(t)
	return nil
}

// SetReadDeadline configures the maximum amount of time that a synchronous Read
// operation can block for. A Read operation will unblock at the point that the
// read deadline is reached if no other condition (such as data arrival or
// connection closure) happens first. If no deadline is configured, Read
// operations can potentially block indefinitely.
func (c *PacketConn) SetReadDeadline(t time.Time) error {
	// TODO warn that this can block while waiting for the Conn actor to run, so don't call it from other actors...
	phony.Block(c, func() { c.readDeadline = &t })
	return nil
}

// SetWriteDeadline configures the maximum amount of time that a synchronous
// Write operation can block for. A Write operation will unblock at the point
// that the read deadline is reached if no other condition (such as data sending
// or connection closure) happens first. If no deadline is configured, Write
// operations can potentially block indefinitely.
func (c *PacketConn) SetWriteDeadline(t time.Time) error {
	// TODO warn that this can block while waiting for the Conn actor to run, so don't call it from other actors...
	phony.Block(c, func() { c.writeDeadline = &t })
	return nil
}

// PacketConnError implements the net.Error interface
type PacketConnError struct {
	error
	timeout   bool
	temporary bool
	closed    bool
	maxsize   int
}

// Timeout returns true if the error relates to a timeout condition on the
// connection.
func (e *PacketConnError) Timeout() bool {
	return e.timeout
}

// Temporary return true if the error is temporary or false if it is a permanent
// error condition.
func (e *PacketConnError) Temporary() bool {
	return e.temporary
}

// PacketTooBig returns in response to sending a packet that is too large, and
// if so, the maximum supported packet size that should be used for the
// connection.
func (e *PacketConnError) PacketTooBig() bool {
	return e.maxsize > 0
}

// PacketMaximumSize returns the maximum supported packet size. This will only
// return a non-zero value if ConnError.PacketTooBig() returns true.
func (e *PacketConnError) PacketMaximumSize() int {
	if !e.PacketTooBig() {
		return 0
	}
	return e.maxsize
}

// Closed returns if the session is already closed and is now unusable.
func (e *PacketConnError) Closed() bool {
	return e.closed
}
