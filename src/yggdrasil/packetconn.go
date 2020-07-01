package yggdrasil

import (
	"errors"
	"net"
	"time"

	"github.com/Arceliar/phony"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/types"
)

type packet struct {
	addr    *crypto.BoxPubKey
	payload []byte
}

type PacketConn struct {
	phony.Inbox
	net.PacketConn
	closed     bool
	readBuffer chan packet
	sessions   *sessions
}

func newPacketConn(ss *sessions) *PacketConn {
	return &PacketConn{
		sessions:   ss,
		readBuffer: make(chan packet),
	}
}

// implements net.PacketConn
func (c *PacketConn) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	if c.closed {
		return 0, nil, PacketConnError{closed: true}
	}
	packet := <-c.readBuffer
	copy(b, packet.payload)
	return len(packet.payload), packet.addr, nil
}

// implements net.PacketConn
func (c *PacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if c.closed {
		return 0, PacketConnError{closed: true}
	}

	boxPubKey, ok := addr.(*crypto.BoxPubKey)
	if !ok {
		return 0, errors.New("expected crypto.BoxPubKey as net.Addr")
	}

	session, ok := c.sessions.getByTheirPerm(boxPubKey)
	if !ok {
		return 0, errors.New("expected a session but there was none")
	}

	err := make(chan error, 1)
	msg := FlowKeyMessage{Message: b}
	nodeID := crypto.GetNodeID(boxPubKey)
	nodeMask := &crypto.NodeID{}
	for i := range nodeMask {
		nodeMask[i] = 0xFF
	}

	session.Act(c, func() {
		// Check if the packet is small enough to go through this session
		sessionMTU := session._getMTU()
		if types.MTU(len(b)) > sessionMTU {
			err <- PacketConnError{maxsize: int(sessionMTU)}
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

		err <- nil
	})

	e := <-err
	return len(b), e
}

// implements net.PacketConn
func (c *PacketConn) Close() error {
	return nil
}

// implements net.PacketConn
func (c *PacketConn) LocalAddr() net.Addr {
	return &c.sessions.router.core.boxPub
}

// implements net.PacketConn
func (c *PacketConn) SetDeadline(t time.Time) error {
	return nil
}

// implements net.PacketConn
func (c *PacketConn) SetReadDeadline(t time.Time) error {
	return nil
}

// implements net.PacketConn
func (c *PacketConn) SetWriteDeadline(t time.Time) error {
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
