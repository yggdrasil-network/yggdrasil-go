package yggdrasil

import (
	"errors"
	"fmt"
	"sync"
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
	readDeadline  time.Time // TODO timer
	writeDeadline time.Time // TODO timer
	expired       bool
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
			c.expired = true
			c.mutex.Unlock()
			return
		}
		if sinfo != nil {
			c.mutex.Lock()
			c.session = sinfo
			c.session.recv = c.recv
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
	err := func() error {
		c.mutex.RLock()
		defer c.mutex.RUnlock()
		if c.expired {
			return errors.New("session is closed")
		}
		return nil
	}()
	if err != nil {
		return 0, err
	}
	select {
	// TODO...
	case p, ok := <-c.recv:
		if !ok {
			c.mutex.Lock()
			c.expired = true
			c.mutex.Unlock()
			return 0, errors.New("session is closed")
		}
		defer util.PutBytes(p.Payload)
		c.mutex.RLock()
		sinfo := c.session
		c.mutex.RUnlock()
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
	var sinfo *sessionInfo
	err = func() error {
		c.mutex.RLock()
		defer c.mutex.RUnlock()
		if c.expired {
			return errors.New("session is closed")
		}
		sinfo = c.session
		return nil
	}()
	if err != nil {
		return 0, err
	}
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
	c.expired = true
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
	c.readDeadline = t
	return nil
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.writeDeadline = t
	return nil
}
