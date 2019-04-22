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
}

func (c *Conn) String() string {
	return fmt.Sprintf("c=%p", c)
}

// This method should only be called from the router goroutine
func (c *Conn) startSearch() {
	searchCompleted := func(sinfo *sessionInfo, err error) {
		if err != nil {
			c.core.log.Debugln("DHT search failed:", err)
			c.mutex.Lock()
			c.expired.Store(true)
			return
		}
		if sinfo != nil {
			c.mutex.Lock()
			c.session = sinfo
			c.nodeID, c.nodeMask = sinfo.theirAddr.GetNodeIDandMask()
			c.mutex.Unlock()
		}
	}
	doSearch := func() {
		sinfo, isIn := c.core.searches.searches[*c.nodeID]
		if !isIn {
			sinfo = c.core.searches.newIterSearch(c.nodeID, c.nodeMask, searchCompleted)
		}
		c.core.searches.continueSearch(sinfo)
	}
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if c.session == nil {
		doSearch()
	} else {
		sinfo := c.session // In case c.session is somehow changed meanwhile
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
	if e, ok := c.expired.Load().(bool); ok && e {
		return 0, errors.New("session is closed")
	}
	c.mutex.RLock()
	sinfo := c.session
	c.mutex.RUnlock()
	select {
	// TODO...
	case p, ok := <-c.recv:
		if !ok {
			c.expired.Store(true)
			return 0, errors.New("session is closed")
		}
		defer util.PutBytes(p.Payload)
		var err error
		sinfo.doWorker(func() {
			if !sinfo.nonceIsOK(&p.Nonce) {
				err = errors.New("packet dropped due to invalid nonce")
				return
			}
			bs, isOK := crypto.BoxOpen(&sinfo.sharedSesKey, p.Payload, &p.Nonce)
			if !isOK {
				util.PutBytes(bs)
				err = errors.New("packet dropped due to decryption failure")
				return
			}
			copy(b, bs)
			if len(bs) < len(b) {
				b = b[:len(bs)]
			}
			sinfo.updateNonce(&p.Nonce)
			sinfo.time = time.Now()
			sinfo.bytesRecvd += uint64(len(b))
		})
		if err != nil {
			return 0, err
		}
		return len(b), nil
		//case <-c.recvTimeout:
		//case <-c.session.closed:
		//	c.expired = true
		//	return len(b), errors.New("session is closed")
	}
}

func (c *Conn) Write(b []byte) (bytesWritten int, err error) {
	if e, ok := c.expired.Load().(bool); ok && e {
		return 0, errors.New("session is closed")
	}
	c.mutex.RLock()
	sinfo := c.session
	c.mutex.RUnlock()
	if sinfo == nil {
		c.core.router.doAdmin(func() {
			c.startSearch()
		})
		return 0, errors.New("searching for remote side")
	}
	//defer util.PutBytes(b)
	var packet []byte
	sinfo.doWorker(func() {
		if !sinfo.init {
			err = errors.New("waiting for remote side to accept " + c.String())
			return
		}
		payload, nonce := crypto.BoxSeal(&sinfo.sharedSesKey, b, &sinfo.myNonce)
		defer util.PutBytes(payload)
		p := wire_trafficPacket{
			Coords:  sinfo.coords,
			Handle:  sinfo.theirHandle,
			Nonce:   *nonce,
			Payload: payload,
		}
		packet = p.encode()
		sinfo.bytesSent += uint64(len(b))
	})
	sinfo.core.router.out(packet)
	return len(b), nil
}

func (c *Conn) Close() error {
	c.expired.Store(true)
	c.session.close()
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
