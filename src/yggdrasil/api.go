package yggdrasil

import (
	"encoding/hex"
	"errors"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

func (c *Core) Dial(network, address string) (Conn, error) {
	conn := Conn{}
	nodeID := crypto.NodeID{}
	nodeMask := crypto.NodeID{}
	// Process
	switch network {
	case "nodeid":
		// A node ID was provided - we don't need to do anything special with it
		dest, err := hex.DecodeString(address)
		if err != nil {
			return Conn{}, err
		}
		copy(nodeID[:], dest)
		for i := range nodeMask {
			nodeMask[i] = 0xFF
		}
	default:
		// An unexpected address type was given, so give up
		return Conn{}, errors.New("unexpected address type")
	}
	conn.core = c
	conn.nodeID = &nodeID
	conn.nodeMask = &nodeMask
	conn.core.router.doAdmin(func() {
		conn.startSearch()
	})
	return conn, nil
}

type Conn struct {
	core          *Core
	nodeID        *crypto.NodeID
	nodeMask      *crypto.NodeID
	session       *sessionInfo
	readDeadline  time.Time
	writeDeadline time.Time
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
			c.core.log.Println("Search from API found", hex.EncodeToString(sinfo.theirPermPub[:]))
		}
	}
	// Try and search for the node on the network
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
		// No or unintiialized session, so we need to search first
		doSearch()
	case time.Since(sinfo.time) > 6*time.Second:
		if sinfo.time.Before(sinfo.pingTime) && time.Since(sinfo.pingTime) > 6*time.Second {
			// We haven't heard from the dest in a while
			// We tried pinging but didn't get a response
			// They may have changed coords
			// Try searching to discover new coords
			// Note that search spam is throttled internally
			doSearch()
		} else {
			// We haven't heard about the dest in a while
			now := time.Now()
			if !sinfo.time.Before(sinfo.pingTime) {
				// Update pingTime to start the clock for searches (above)
				sinfo.pingTime = now
			}
			if time.Since(sinfo.pingSend) > time.Second {
				// Send at most 1 ping per second
				sinfo.pingSend = now
				c.core.sessions.sendPingPong(sinfo, false)
			}
		}
	}
}

func (c *Conn) Read(b []byte) (int, error) {
	if c.session == nil {
		return 0, errors.New("invalid session")
	}
	p := <-c.session.recv
	defer util.PutBytes(p.Payload)
	if !c.session.nonceIsOK(&p.Nonce) {
		return 0, errors.New("invalid nonce")
	}
	bs, isOK := crypto.BoxOpen(&c.session.sharedSesKey, p.Payload, &p.Nonce)
	if !isOK {
		util.PutBytes(bs)
		return 0, errors.New("failed to decrypt")
	}
	b = b[:0]
	b = append(b, bs...)
	c.session.updateNonce(&p.Nonce)
	c.session.time = time.Now()
	c.session.bytesRecvd += uint64(len(bs))
	return len(b), nil
}

func (c *Conn) Write(b []byte) (int, error) {
	if c.session == nil {
		c.core.router.doAdmin(func() {
			c.startSearch()
		})
		return 0, errors.New("invalid session")
	}
	defer util.PutBytes(b)
	if !c.session.init {
		// To prevent using empty session keys
		return 0, errors.New("session not initialised")
	}
	// code isn't multithreaded so appending to this is safe
	coords := c.session.coords
	// Prepare the payload
	payload, nonce := crypto.BoxSeal(&c.session.sharedSesKey, b, &c.session.myNonce)
	defer util.PutBytes(payload)
	p := wire_trafficPacket{
		Coords:  coords,
		Handle:  c.session.theirHandle,
		Nonce:   *nonce,
		Payload: payload,
	}
	packet := p.encode()
	c.session.bytesSent += uint64(len(b))
	c.session.send <- packet
	//c.session.core.router.out(packet)
	return len(b), nil
}

func (c *Conn) Close() error {
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
