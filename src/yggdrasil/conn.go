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

// Error implements the net.Error interface
type ConnError struct {
	error
	timeout   bool
	temporary bool
}

func (e *ConnError) Timeout() bool {
	return e.timeout
}

func (e *ConnError) Temporary() bool {
	return e.temporary
}

type Conn struct {
	core          *Core
	nodeID        *crypto.NodeID
	nodeMask      *crypto.NodeID
	mutex         sync.RWMutex
	closed        bool
	session       *sessionInfo
	readDeadline  atomic.Value  // time.Time // TODO timer
	writeDeadline atomic.Value  // time.Time // TODO timer
	searching     atomic.Value  // bool
	searchwait    chan struct{} // Never reset this, it's only used for the initial search
}

// TODO func NewConn() that initializes additional fields as needed
func newConn(core *Core, nodeID *crypto.NodeID, nodeMask *crypto.NodeID, session *sessionInfo) *Conn {
	conn := Conn{
		core:       core,
		nodeID:     nodeID,
		nodeMask:   nodeMask,
		session:    session,
		searchwait: make(chan struct{}),
	}
	conn.searching.Store(false)
	return &conn
}

func (c *Conn) String() string {
	return fmt.Sprintf("conn=%p", c)
}

// This method should only be called from the router goroutine
func (c *Conn) startSearch() {
	// The searchCompleted callback is given to the search
	searchCompleted := func(sinfo *sessionInfo, err error) {
		defer c.searching.Store(false)
		// If the search failed for some reason, e.g. it hit a dead end or timed
		// out, then do nothing
		if err != nil {
			c.core.log.Debugln(c.String(), "DHT search failed:", err)
			go func() {
				time.Sleep(time.Second)
				c.mutex.RLock()
				closed := c.closed
				c.mutex.RUnlock()
				if !closed {
					// Restart the search, or else Write can stay blocked forever
					c.core.router.admin <- c.startSearch
				}
			}()
			return
		}
		// Take the connection mutex
		c.mutex.Lock()
		defer c.mutex.Unlock()
		// Were we successfully given a sessionInfo pointer?
		if sinfo != nil {
			// Store it, and update the nodeID and nodeMask (which may have been
			// wildcarded before now) with their complete counterparts
			c.core.log.Debugln(c.String(), "DHT search completed")
			c.session = sinfo
			c.nodeID = crypto.GetNodeID(&sinfo.theirPermPub)
			for i := range c.nodeMask {
				c.nodeMask[i] = 0xFF
			}
			// Make sure that any blocks on read/write operations are lifted
			defer func() { recover() }() // So duplicate searches don't panic
			close(c.searchwait)
		} else {
			// No session was returned - this shouldn't really happen because we
			// should always return an error reason if we don't return a session
			panic("DHT search didn't return an error or a sessionInfo")
		}
		if c.closed {
			// Things were closed before the search returned
			// Go ahead and close it again to make sure the session is cleaned up
			go c.Close()
		}
	}
	// doSearch will be called below in response to one or more conditions
	doSearch := func() {
		c.searching.Store(true)
		// Check to see if there is a search already matching the destination
		sinfo, isIn := c.core.searches.searches[*c.nodeID]
		if !isIn {
			// Nothing was found, so create a new search
			sinfo = c.core.searches.newIterSearch(c.nodeID, c.nodeMask, searchCompleted)
			c.core.log.Debugf("%s DHT search started: %p", c.String(), sinfo)
		}
		// Continue the search
		c.core.searches.continueSearch(sinfo)
	}
	// Take a copy of the session object, in case it changes later
	c.mutex.RLock()
	sinfo := c.session
	c.mutex.RUnlock()
	if c.session == nil {
		// No session object is present so previous searches, if we ran any, have
		// not yielded a useful result (dead end, remote host not found)
		doSearch()
	} else {
		sinfo.worker <- func() {
			switch {
			case !sinfo.init:
				doSearch()
			case time.Since(sinfo.time) > 6*time.Second:
				if sinfo.time.Before(sinfo.pingTime) && time.Since(sinfo.pingTime) > 6*time.Second {
					// TODO double check that the above condition is correct
					doSearch()
				} else {
					c.core.sessions.ping(sinfo)
				}
			default: // Don't do anything, to keep traffic throttled
			}
		}
	}
}

func getDeadlineTimer(value *atomic.Value) *time.Timer {
	timer := time.NewTimer(0)
	util.TimerStop(timer)
	if deadline, ok := value.Load().(time.Time); ok {
		timer.Reset(time.Until(deadline))
	}
	return timer
}

func (c *Conn) Read(b []byte) (int, error) {
	// Take a copy of the session object
	c.mutex.RLock()
	sinfo := c.session
	c.mutex.RUnlock()
	timer := getDeadlineTimer(&c.readDeadline)
	defer util.TimerStop(timer)
	// If there is a search in progress then wait for the result
	if sinfo == nil {
		// Wait for the search to complete
		select {
		case <-c.searchwait:
		case <-timer.C:
			return 0, ConnError{errors.New("Timeout"), true, false}
		}
		// Retrieve our session info again
		c.mutex.RLock()
		sinfo = c.session
		c.mutex.RUnlock()
		// If sinfo is still nil at this point then the search failed and the
		// searchwait channel has been recreated, so might as well give up and
		// return an error code
		if sinfo == nil {
			return 0, errors.New("search failed")
		}
	}
	// Wait for some traffic to come through from the session
	select {
	case p, ok := <-sinfo.recv:
		// If the session is closed then do nothing
		if !ok {
			return 0, errors.New("session is closed")
		}
		defer util.PutBytes(p.Payload)
		var err error
		done := make(chan struct{})
		workerFunc := func() {
			defer close(done)
			// If the nonce is bad then drop the packet and return an error
			if !sinfo.nonceIsOK(&p.Nonce) {
				err = errors.New("packet dropped due to invalid nonce")
				return
			}
			// Decrypt the packet
			bs, isOK := crypto.BoxOpen(&sinfo.sharedSesKey, p.Payload, &p.Nonce)
			// Check if we were unable to decrypt the packet for some reason and
			// return an error if we couldn't
			if !isOK {
				util.PutBytes(bs)
				err = errors.New("packet dropped due to decryption failure")
				return
			}
			// Return the newly decrypted buffer back to the slice we were given
			copy(b, bs)
			// Trim the slice down to size based on the data we received
			if len(bs) < len(b) {
				b = b[:len(bs)]
			}
			// Update the session
			sinfo.updateNonce(&p.Nonce)
			sinfo.time = time.Now()
			sinfo.bytesRecvd += uint64(len(b))
		}
		// Hand over to the session worker
		select { // Send to worker
		case sinfo.worker <- workerFunc:
		case <-timer.C:
			return 0, ConnError{errors.New("Timeout"), true, false}
		}
		select { // Wait for worker to return
		case <-done:
		case <-timer.C:
			return 0, ConnError{errors.New("Timeout"), true, false}
		}
		// Something went wrong in the session worker so abort
		if err != nil {
			return 0, err
		}
		// If we've reached this point then everything went to plan, return the
		// number of bytes we populated back into the given slice
		return len(b), nil
	}
}

func (c *Conn) Write(b []byte) (bytesWritten int, err error) {
	c.mutex.RLock()
	sinfo := c.session
	c.mutex.RUnlock()
	timer := getDeadlineTimer(&c.writeDeadline)
	defer util.TimerStop(timer)
	// If the session doesn't exist, or isn't initialised (which probably means
	// that the search didn't complete successfully) then we may need to wait for
	// the search to complete or start the search again
	if sinfo == nil || !sinfo.init {
		// Is a search already taking place?
		if searching, sok := c.searching.Load().(bool); !sok || (sok && !searching) {
			// No search was already taking place so start a new one
			c.core.router.doAdmin(func() {
				c.startSearch()
			})
		}
		// Wait for the search to complete
		select {
		case <-c.searchwait:
		case <-timer.C:
			return 0, ConnError{errors.New("Timeout"), true, false}
		}
		// Retrieve our session info again
		c.mutex.RLock()
		sinfo = c.session
		c.mutex.RUnlock()
		// If sinfo is still nil at this point then the search failed and the
		// searchwait channel has been recreated, so might as well give up and
		// return an error code
		if sinfo == nil {
			return 0, errors.New("search failed")
		}
	}
	// defer util.PutBytes(b)
	var packet []byte
	done := make(chan struct{})
	workerFunc := func() {
		defer close(done)
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
	}
	// Hand over to the session worker
	select { // Send to worker
	case sinfo.worker <- workerFunc:
	case <-timer.C:
		return 0, ConnError{errors.New("Timeout"), true, false}
	}
	select { // Wait for worker to return
	case <-done:
	case <-timer.C:
		return 0, ConnError{errors.New("Timeout"), true, false}
	}
	// Give the packet to the router
	sinfo.core.router.out(packet)
	// Finally return the number of bytes we wrote
	return len(b), nil
}

func (c *Conn) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.session != nil {
		// Close the session, if it hasn't been closed already
		c.session.close()
		c.session = nil
	}
	// This can't fail yet - TODO?
	c.closed = true
	return nil
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
