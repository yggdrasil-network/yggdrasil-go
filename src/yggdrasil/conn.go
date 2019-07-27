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
func (e *ConnError) PacketTooBig() (bool, int) {
	return e.maxsize > 0, e.maxsize
}

// Closed returns if the session is already closed and is now unusable.
func (e *ConnError) Closed() bool {
	return e.closed
}

type Conn struct {
	core          *Core
	readDeadline  atomic.Value // time.Time // TODO timer
	writeDeadline atomic.Value // time.Time // TODO timer
	cancel        util.Cancellation
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
		cancel:   util.NewCancellation(),
	}
	return &conn
}

func (c *Conn) String() string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return fmt.Sprintf("conn=%p", c)
}

// This should never be called from the router goroutine
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

func (c *Conn) getDeadlineCancellation(value *atomic.Value) util.Cancellation {
	if deadline, ok := value.Load().(time.Time); ok {
		// A deadline is set, so return a Cancellation that uses it
		return util.CancellationWithDeadline(c.cancel, deadline)
	} else {
		// No cancellation was set, so return a child cancellation with no timeout
		return util.CancellationChild(c.cancel)
	}
}

func (c *Conn) Read(b []byte) (int, error) {
	// Take a copy of the session object
	sinfo := c.session
	cancel := c.getDeadlineCancellation(&c.readDeadline)
	defer cancel.Cancel(nil)
	var bs []byte
	for {
		// Wait for some traffic to come through from the session
		select {
		case <-cancel.Finished():
			if cancel.Error() == util.CancellationTimeoutError {
				return 0, ConnError{errors.New("read timeout"), true, false, false, 0}
			} else {
				return 0, ConnError{errors.New("session closed"), false, false, true, 0}
			}
		case p, ok := <-sinfo.recv:
			// If the session is closed then do nothing
			if !ok {
				return 0, ConnError{errors.New("session closed"), false, false, true, 0}
			}
			var err error
			sessionFunc := func() {
				defer util.PutBytes(p.Payload)
				// If the nonce is bad then drop the packet and return an error
				if !sinfo.nonceIsOK(&p.Nonce) {
					err = ConnError{errors.New("packet dropped due to invalid nonce"), false, true, false, 0}
					return
				}
				// Decrypt the packet
				var isOK bool
				bs, isOK = crypto.BoxOpen(&sinfo.sharedSesKey, p.Payload, &p.Nonce)
				// Check if we were unable to decrypt the packet for some reason and
				// return an error if we couldn't
				if !isOK {
					err = ConnError{errors.New("packet dropped due to decryption failure"), false, true, false, 0}
					return
				}
				// Update the session
				sinfo.updateNonce(&p.Nonce)
				sinfo.time = time.Now()
				sinfo.bytesRecvd += uint64(len(bs))
			}
			sinfo.doFunc(sessionFunc)
			// Something went wrong in the session worker so abort
			if err != nil {
				if ce, ok := err.(*ConnError); ok && ce.Temporary() {
					continue
				}
				return 0, err
			}
			// Copy results to the output slice and clean up
			copy(b, bs)
			util.PutBytes(bs)
			// If we've reached this point then everything went to plan, return the
			// number of bytes we populated back into the given slice
			return len(bs), nil
		}
	}
}

func (c *Conn) Write(b []byte) (bytesWritten int, err error) {
	sinfo := c.session
	var packet []byte
	written := len(b)
	sessionFunc := func() {
		// Does the packet exceed the permitted size for the session?
		if uint16(len(b)) > sinfo.getMTU() {
			written, err = 0, ConnError{errors.New("packet too big"), true, false, false, int(sinfo.getMTU())}
			return
		}
		// Encrypt the packet
		payload, nonce := crypto.BoxSeal(&sinfo.sharedSesKey, b, &sinfo.myNonce)
		defer util.PutBytes(payload)
		// Construct the wire packet to send to the router
		p := wire_trafficPacket{
			Coords:  sinfo.coords,
			Handle:  sinfo.theirHandle,
			Nonce:   *nonce,
			Payload: payload,
		}
		packet = p.encode()
		sinfo.bytesSent += uint64(len(b))
		// The rest of this work is session keep-alive traffic
		doSearch := func() {
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
		switch {
		case time.Since(sinfo.time) > 6*time.Second:
			if sinfo.time.Before(sinfo.pingTime) && time.Since(sinfo.pingTime) > 6*time.Second {
				// TODO double check that the above condition is correct
				doSearch()
			} else {
				sinfo.core.sessions.ping(sinfo)
			}
		case sinfo.reset && sinfo.pingTime.Before(sinfo.time):
			sinfo.core.sessions.ping(sinfo)
		default: // Don't do anything, to keep traffic throttled
		}
	}
	sinfo.doFunc(sessionFunc)
	// Give the packet to the router
	if written > 0 {
		sinfo.core.router.out(packet)
	}
	// Finally return the number of bytes we wrote
	return written, err
}

func (c *Conn) Close() (err error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.session != nil {
		// Close the session, if it hasn't been closed already
		c.core.router.doAdmin(c.session.close)
	}
	if e := c.cancel.Cancel(errors.New("connection closed")); e != nil {
		err = ConnError{errors.New("close failed, session already closed"), false, false, true, 0}
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
