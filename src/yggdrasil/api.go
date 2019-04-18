package yggdrasil

import (
	"encoding/hex"
	"errors"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

func (c *Core) Dial(network, address string) (Conn, error) {
	var nodeID *crypto.NodeID
	var nodeMask *crypto.NodeID
	// Process
	switch network {
	case "nodeid":
		// A node ID was provided - we don't need to do anything special with it
		dest, err := hex.DecodeString(address)
		if err != nil {
			return Conn{}, err
		}
		copy(nodeID[:], dest)
		var m crypto.NodeID
		for i := range dest {
			m[i] = 0xFF
		}
		copy(nodeMask[:], m[:])
	default:
		// An unexpected address type was given, so give up
		return Conn{}, errors.New("unexpected address type")
	}
	// Try and search for the node on the network
	doSearch := func() {
		sinfo, isIn := c.searches.searches[*nodeID]
		if !isIn {
			sinfo = c.searches.newIterSearch(nodeID, nodeMask)
		}
		c.searches.continueSearch(sinfo)
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
				c.sessions.sendPingPong(sinfo, false)
			}
		}
	}
	return Conn{
		session: sinfo,
	}, nil
}

type Conn struct {
	session       *sessionInfo
	readDeadline  time.Time
	writeDeadline time.Time
}

func (c *Conn) Read(b []byte) (int, error) {
	return 0, nil
}

func (c *Conn) Write(b []byte) (int, error) {
	return 0, nil
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
