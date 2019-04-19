package yggdrasil

import (
	"encoding/hex"
	"errors"
	"sync/atomic"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

type Conn struct {
	core          *Core
	nodeID        *crypto.NodeID
	nodeMask      *crypto.NodeID
	session       *sessionInfo
	readDeadline  time.Time
	writeDeadline time.Time
	expired       bool
}

// This method should only be called from the router goroutine
func (c *Conn) startSearch() {
	searchCompleted := func(sinfo *sessionInfo, err error) {
		if err != nil {
			c.core.log.Debugln("DHT search failed:", err)
			return
		}
		if sinfo != nil {
			c.session = sinfo
		}
	}
	doSearch := func() {
		sinfo, isIn := c.core.searches.searches[*c.nodeID]
		if !isIn {
			c.core.log.Debugln("Starting search for", hex.EncodeToString(c.nodeID[:]))
			sinfo = c.core.searches.newIterSearch(c.nodeID, c.nodeMask, searchCompleted)
		}
		c.core.searches.continueSearch(sinfo)
	}
	var sinfo *sessionInfo
	var isIn bool
	switch {
	case !isIn || !sinfo.init:
		doSearch()
	case time.Since(sinfo.time) > 6*time.Second:
		if sinfo.time.Before(sinfo.pingTime) && time.Since(sinfo.pingTime) > 6*time.Second {
			doSearch()
		} else {
			now := time.Now()
			if !sinfo.time.Before(sinfo.pingTime) {
				sinfo.pingTime = now
			}
			if time.Since(sinfo.pingSend) > time.Second {
				sinfo.pingSend = now
				c.core.sessions.sendPingPong(sinfo, false)
			}
		}
	}
}

func (c *Conn) Read(b []byte) (int, error) {
	if c.expired {
		return 0, errors.New("session is closed")
	}
	if c.session == nil {
		return 0, errors.New("searching for remote side")
	}
	if !c.session.init {
		return 0, errors.New("waiting for remote side to accept")
	}
	select {
	case p, ok := <-c.session.recv:
		if !ok {
			c.expired = true
			return 0, errors.New("session is closed")
		}
		defer util.PutBytes(p.Payload)
		err := func() error {
			c.session.theirNonceMutex.Lock()
			defer c.session.theirNonceMutex.Unlock()
			if !c.session.nonceIsOK(&p.Nonce) {
				return errors.New("packet dropped due to invalid nonce")
			}
			bs, isOK := crypto.BoxOpen(&c.session.sharedSesKey, p.Payload, &p.Nonce)
			if !isOK {
				util.PutBytes(bs)
				return errors.New("packet dropped due to decryption failure")
			}
			copy(b, bs)
			if len(bs) < len(b) {
				b = b[:len(bs)]
			}
			c.session.updateNonce(&p.Nonce)
			c.session.time = time.Now()
			return nil
		}()
		if err != nil {
			return 0, err
		}
		atomic.AddUint64(&c.session.bytesRecvd, uint64(len(b)))
		return len(b), nil
	case <-c.session.closed:
		c.expired = true
		return len(b), errors.New("session is closed")
	}
}

func (c *Conn) Write(b []byte) (bytesWritten int, err error) {
	if c.expired {
		return 0, errors.New("session is closed")
	}
	if c.session == nil {
		c.core.router.doAdmin(func() {
			c.startSearch()
		})
		return 0, errors.New("searching for remote side")
	}
	defer util.PutBytes(b)
	if !c.session.init {
		// To prevent using empty session keys
		return 0, errors.New("waiting for remote side to accept")
	}
	// code isn't multithreaded so appending to this is safe
	coords := c.session.coords
	// Prepare the payload
	c.session.myNonceMutex.Lock()
	payload, nonce := crypto.BoxSeal(&c.session.sharedSesKey, b, &c.session.myNonce)
	c.session.myNonceMutex.Unlock()
	defer util.PutBytes(payload)
	p := wire_trafficPacket{
		Coords:  coords,
		Handle:  c.session.theirHandle,
		Nonce:   *nonce,
		Payload: payload,
	}
	packet := p.encode()
	atomic.AddUint64(&c.session.bytesSent, uint64(len(b)))
	select {
	case c.session.send <- packet:
	case <-c.session.closed:
		c.expired = true
		return len(b), errors.New("session is closed")
	}
	c.session.core.router.out(packet)
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
	return *crypto.GetNodeID(&c.session.theirPermPub)
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
