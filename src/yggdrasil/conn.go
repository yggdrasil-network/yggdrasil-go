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
	searching     atomic.Value // bool
}

func (c *Conn) String() string {
	return fmt.Sprintf("conn=%p", c)
}

// This method should only be called from the router goroutine
func (c *Conn) startSearch() {
	// The searchCompleted callback is given to the search
	searchCompleted := func(sinfo *sessionInfo, err error) {
		// Update the connection with the fact that the search completed, which
		// allows another search to be triggered if necessary
		c.searching.Store(false)
		// If the search failed for some reason, e.g. it hit a dead end or timed
		// out, then do nothing
		if err != nil {
			c.core.log.Debugln(c.String(), "DHT search failed:", err)
			return
		}
		// Take the connection mutex
		c.mutex.Lock()
		defer c.mutex.Unlock()
		// Were we successfully given a sessionInfo pointeR?
		if sinfo != nil {
			// Store it, and update the nodeID and nodeMask (which may have been
			// wildcarded before now) with their complete counterparts
			c.core.log.Debugln(c.String(), "DHT search completed")
			c.session = sinfo
			c.nodeID = crypto.GetNodeID(&sinfo.theirPermPub)
			for i := range c.nodeMask {
				c.nodeMask[i] = 0xFF
			}
		} else {
			// No session was returned - this shouldn't really happen because we
			// should always return an error reason if we don't return a session
			panic("DHT search didn't return an error or a sessionInfo")
		}
	}
	// doSearch will be called below in response to one or more conditions
	doSearch := func() {
		// Store the fact that we're searching, so that we don't start additional
		// searches until this one has completed
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

func (c *Conn) Read(b []byte) (int, error) {
	// Take a copy of the session object
	c.mutex.RLock()
	sinfo := c.session
	c.mutex.RUnlock()
	// If the session is not initialised, do nothing. Currently in this instance
	// in a write, we would trigger a new session, but it doesn't make sense for
	// us to block forever here if the session will not reopen.
	// TODO: should this return an error or just a zero-length buffer?
	if sinfo == nil || !sinfo.init {
		return 0, errors.New("session is closed")
	}
	// Wait for some traffic to come through from the session
	select {
	// TODO...
	case p, ok := <-c.recv:
		// If the session is closed then do nothing
		if !ok {
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
	// If the session doesn't exist, or isn't initialised (which probably means
	// that the search didn't complete successfully) then try to search again
	if sinfo == nil || !sinfo.init {
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
	// defer util.PutBytes(b)
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
	// Close the session, if it hasn't been closed already
	c.session.close()
	c.session = nil
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
