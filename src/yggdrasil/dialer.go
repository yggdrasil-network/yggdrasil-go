package yggdrasil

import (
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

// Dialer represents an Yggdrasil connection dialer.
type Dialer struct {
	core *Core
}

// TODO DialContext that allows timeouts/cancellation, Dial should just call this with no timeout set in the context

// Dial opens a session to the given node. The first paramter should be "nodeid"
// and the second parameter should contain a hexadecimal representation of the
// target node ID.
func (d *Dialer) Dial(network, address string) (*Conn, error) {
	var nodeID crypto.NodeID
	var nodeMask crypto.NodeID
	// Process
	switch network {
	case "nodeid":
		// A node ID was provided - we don't need to do anything special with it
		if tokens := strings.Split(address, "/"); len(tokens) == 2 {
			len, err := strconv.Atoi(tokens[1])
			if err != nil {
				return nil, err
			}
			dest, err := hex.DecodeString(tokens[0])
			if err != nil {
				return nil, err
			}
			copy(nodeID[:], dest)
			for idx := 0; idx < len; idx++ {
				nodeMask[idx/8] |= 0x80 >> byte(idx%8)
			}
		} else {
			dest, err := hex.DecodeString(tokens[0])
			if err != nil {
				return nil, err
			}
			copy(nodeID[:], dest)
			for i := range nodeMask {
				nodeMask[i] = 0xFF
			}
		}
		return d.DialByNodeIDandMask(&nodeID, &nodeMask)
	default:
		// An unexpected address type was given, so give up
		return nil, errors.New("unexpected address type")
	}
}

// DialByNodeIDandMask opens a session to the given node based on raw
// NodeID parameters.
func (d *Dialer) DialByNodeIDandMask(nodeID, nodeMask *crypto.NodeID) (*Conn, error) {
	conn := newConn(d.core, nodeID, nodeMask, nil)
	if err := conn.search(); err != nil {
		conn.Close()
		return nil, err
	}
	t := time.NewTimer(6 * time.Second) // TODO use a context instead
	defer t.Stop()
	select {
	case <-conn.session.init:
		conn.session.startWorkers()
		return conn, nil
	case <-t.C:
		conn.Close()
		return nil, errors.New("session handshake timeout")
	}
}
