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

type Conn struct {
	core          *Core
	nodeID        *crypto.NodeID
	nodeMask      *crypto.NodeID
	recv          chan *wire_trafficPacket // Eventually gets attached to session.recv
	mutex         *sync.RWMutex
	session       *sessionInfo
	readDeadline  atomic.Value // time.Time // TODO timer
	writeDeadline atomic.Value // time.Time // TODO timer
	expired       atomic.Value // bool
	searching     atomic.Value // bool
}

func (c *Conn) String() string {
	return fmt.Sprintf("conn=%p", c)
}

// This method should only be called from the router goroutine
func (c *Conn) startSearch() {
	searchCompleted := func(sinfo *sessionInfo, err error) {
		c.searching.Store(false)
		c.mutex.Lock()
		defer c.mutex.Unlock()
		if err != nil {
			c.core.log.Debugln(c.String(), "DHT search failed:", err)
			c.expired.Store(true)
			return
		}
		if sinfo != nil {
			c.core.log.Debugln(c.String(), "DHT search completed")
			c.session = sinfo
			c.nodeID, c.nodeMask = sinfo.theirAddr.GetNodeIDandMask()
			c.expired.Store(false)
		} else {
			c.core.log.Debugln(c.String(), "DHT search failed: no session returned")
			c.expired.Store(true)
			return
		}
	}
	doSearch := func() {
		c.searching.Store(true)
		sinfo, isIn := c.core.searches.searches[*c.nodeID]
		if !isIn {
			sinfo = c.core.searches.newIterSearch(c.nodeID, c.nodeMask, searchCompleted)
			c.core.log.Debugf("%s DHT search started: %p", c.String(), sinfo)
		}
		c.core.searches.continueSearch(sinfo)
	}
	c.mutex.RLock()
	sinfo := c.session
	c.mutex.RUnlock()
	if c.session == nil {
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

func (c *Conn) Read(b []byte) (int, error) {
	// If the session is marked as expired then do nothing at this point
	if e, ok := c.expired.Load().(bool); ok && e {
		return 0, errors.New("session is closed")
	}
	// Take a copy of the session object
	c.mutex.RLock()
	sinfo := c.session
	c.mutex.RUnlock()
	// If the session is not initialised, do nothing. Currently in this instance
	// in a write, we would trigger a new session, but it doesn't make sense for
	// us to block forever here if the session will not reopen.
	// TODO: should this return an error or just a zero-length buffer?
	if !sinfo.init {
		return 0, errors.New("session is closed")
	}
	// Wait for some traffic to come through from the session
	select {
	// TODO...
	case p, ok := <-c.recv:
		// If the channel was closed then mark the connection as expired, this will
		// mean that the next write will start a new search and reopen the session
		if !ok {
			c.expired.Store(true)
			return 0, errors.New("session is closed")
		}
		defer util.PutBytes(p.Payload)
		var err error
		// Hand over to the session worker
		sinfo.doWorker(func() {
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
		})
		// Something went wrong in the session worker so abort
		if err != nil {
			return 0, err
		}
		// If we've reached this point then everything went to plan, return the
		// number of bytes we populated back into the given slice
		return len(b), nil
		//case <-c.recvTimeout:
		//case <-c.session.closed:
		//	c.expired = true
		//	return len(b), errors.New("session is closed")
	}
}

func (c *Conn) Write(b []byte) (bytesWritten int, err error) {
	c.mutex.RLock()
	sinfo := c.session
	c.mutex.RUnlock()
	// Check whether the connection is expired, if it is we can start a new
	// search to revive it
	expired, eok := c.expired.Load().(bool)
	// If the session doesn't exist, or isn't initialised (which probably means
	// that the session was never set up or it closed by timeout), or the conn
	// is marked as expired, then see if we can start a new search
	if sinfo == nil || !sinfo.init || (eok && expired) {
		// Is a search already taking place?
		if searching, sok := c.searching.Load().(bool); !sok || (sok && !searching) {
			// No search was already taking place so start a new one
			c.core.router.doAdmin(func() {
				c.startSearch()
			})
			return 0, errors.New("starting search")
		}
		// A search is already taking place so wait for it to finish
		return 0, errors.New("waiting for search to complete")
	}
	//defer util.PutBytes(b)
	var packet []byte
	// Hand over to the session worker
	sinfo.doWorker(func() {
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
	})
	// Give the packet to the router
	sinfo.core.router.out(packet)
	// Finally return the number of bytes we wrote
	return len(b), nil
}

func (c *Conn) Close() error {
	// Mark the connection as expired, so that a future read attempt will fail
	// and a future write attempt will start a new search
	c.expired.Store(true)
	// Close the session, if it hasn't been closed already
	c.session.close()
	// This can't fail yet - TODO?
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
