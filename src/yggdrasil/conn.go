package yggdrasil

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
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
	core          *Core
	readDeadline  atomic.Value // time.Time // TODO timer
	writeDeadline atomic.Value // time.Time // TODO timer
	mutex         sync.RWMutex // protects the below
	nodeID        *crypto.NodeID
	nodeMask      *crypto.NodeID
	session       *sessionInfo
}

// TODO func NewConn() that initializes additional fields as needed
func newConn(core *Core, nodeID *crypto.NodeID, nodeMask *crypto.NodeID, session *sessionInfo) *Conn {
	conn := Conn{
		core:     core,
		nodeID:   nodeID,
		nodeMask: nodeMask,
		session:  session,
	}
	return &conn
}

func (c *Conn) String() string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return fmt.Sprintf("conn=%p", c)
}

// This should never be called from the router goroutine, used in the dial functions
func (c *Conn) search() error {
	var sinfo *searchInfo
	var isIn bool
	c.core.router.doAdmin(func() { sinfo, isIn = c.core.searches.searches[*c.nodeID] })
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
			sinfo = c.core.searches.newIterSearch(c.nodeID, c.nodeMask, searchCompleted)
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
		}
		return err
	} else {
		return errors.New("search already exists")
	}
	return nil
}

// Used in session keep-alive traffic in Conn.Write
func (c *Conn) doSearch() {
	routerWork := func() {
		// Check to see if there is a search already matching the destination
		sinfo, isIn := c.core.searches.searches[*c.nodeID]
		if !isIn {
			// Nothing was found, so create a new search
			searchCompleted := func(sinfo *sessionInfo, e error) {}
			sinfo = c.core.searches.newIterSearch(c.nodeID, c.nodeMask, searchCompleted)
			c.core.log.Debugf("%s DHT search started: %p", c.String(), sinfo)
		}
		// Continue the search
		sinfo.continueSearch()
	}
	go func() { c.core.router.admin <- routerWork }()
}

func (c *Conn) getDeadlineCancellation(value *atomic.Value) util.Cancellation {
	if deadline, ok := value.Load().(time.Time); ok {
		// A deadline is set, so return a Cancellation that uses it
		return util.CancellationWithDeadline(c.session.cancel, deadline)
	} else {
		// No cancellation was set, so return a child cancellation with no timeout
		return util.CancellationChild(c.session.cancel)
	}
}

// Used internally by Read, the caller is responsible for util.PutBytes when they're done.
func (c *Conn) ReadNoCopy() ([]byte, error) {
	cancel := c.getDeadlineCancellation(&c.readDeadline)
	defer cancel.Cancel(nil)
	// Wait for some traffic to come through from the session
	select {
	case <-cancel.Finished():
		if cancel.Error() == util.CancellationTimeoutError {
			return nil, ConnError{errors.New("read timeout"), true, false, false, 0}
		} else {
			return nil, ConnError{errors.New("session closed"), false, false, true, 0}
		}
	case bs := <-c.session.recv:
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

// Used internally by Write, the caller must not reuse the argument bytes when no error occurs
func (c *Conn) WriteNoCopy(bs []byte) error {
	var err error
	sessionFunc := func() {
		// Does the packet exceed the permitted size for the session?
		if uint16(len(bs)) > c.session.getMTU() {
			err = ConnError{errors.New("packet too big"), true, false, false, int(c.session.getMTU())}
			return
		}
		// The rest of this work is session keep-alive traffic
		switch {
		case time.Since(c.session.time) > 6*time.Second:
			if c.session.time.Before(c.session.pingTime) && time.Since(c.session.pingTime) > 6*time.Second {
				// TODO double check that the above condition is correct
				c.doSearch()
			} else {
				c.core.sessions.ping(c.session)
			}
		case c.session.reset && c.session.pingTime.Before(c.session.time):
			c.core.sessions.ping(c.session)
		default: // Don't do anything, to keep traffic throttled
		}
	}
	c.session.doFunc(sessionFunc)
	if err == nil {
		cancel := c.getDeadlineCancellation(&c.writeDeadline)
		defer cancel.Cancel(nil)
		select {
		case <-cancel.Finished():
			if cancel.Error() == util.CancellationTimeoutError {
				err = ConnError{errors.New("write timeout"), true, false, false, 0}
			} else {
				err = ConnError{errors.New("session closed"), false, false, true, 0}
			}
		case c.session.send <- bs:
		}
	}
	return err
}

// Implements net.Conn.Write
func (c *Conn) Write(b []byte) (int, error) {
	written := len(b)
	bs := append(util.GetBytes(), b...)
	err := c.WriteNoCopy(bs)
	if err != nil {
		util.PutBytes(bs)
		written = 0
	}
	return written, err
}

func (c *Conn) Close() (err error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.session != nil {
		// Close the session, if it hasn't been closed already
		if e := c.session.cancel.Cancel(errors.New("connection closed")); e != nil {
			err = ConnError{errors.New("close failed, session already closed"), false, false, true, 0}
		}
	}
	return
}

func (c *Conn) LocalAddr() crypto.NodeID {
	return *crypto.GetNodeID(&c.session.core.boxPub)
}

func (c *Conn) RemoteAddr() crypto.NodeID {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return *c.nodeID
}

func (c *Conn) SetDeadline(t time.Time) error {
	c.SetReadDeadline(t)
	c.SetWriteDeadline(t)
	return nil
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	c.readDeadline.Store(t)
	return nil
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.writeDeadline.Store(t)
	return nil
}
