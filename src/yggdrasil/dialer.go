package yggdrasil

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

// Dialer represents an Yggdrasil connection dialer.
type Dialer struct {
	core *Core
}

// Dial opens a session to the given node. The first parameter should be "nodeid"
// and the second parameter should contain a hexadecimal representation of the
// target node ID. It uses DialContext internally.
func (d *Dialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(nil, network, address)
}

// DialContext is used internally by Dial, and should only be used with a context that includes a timeout. It uses DialByNodeIDandMask internally.
func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var nodeID crypto.NodeID
	var nodeMask crypto.NodeID
	// Process
	switch network {
	case "nodeid":
		// A node ID was provided - we don't need to do anything special with it
		if tokens := strings.Split(address, "/"); len(tokens) == 2 {
			l, err := strconv.Atoi(tokens[1])
			if err != nil {
				return nil, err
			}
			dest, err := hex.DecodeString(tokens[0])
			if err != nil {
				return nil, err
			}
			copy(nodeID[:], dest)
			for idx := 0; idx < l; idx++ {
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
		return d.DialByNodeIDandMask(ctx, &nodeID, &nodeMask)
	default:
		// An unexpected address type was given, so give up
		return nil, errors.New("unexpected address type")
	}
}

// DialByNodeIDandMask opens a session to the given node based on raw
// NodeID parameters. If ctx is nil or has no timeout, then a default timeout of 6 seconds will apply, beginning *after* the search finishes.
func (d *Dialer) DialByNodeIDandMask(ctx context.Context, nodeID, nodeMask *crypto.NodeID) (net.Conn, error) {
	conn := newConn(d.core, nodeID, nodeMask, nil)
	if err := conn.search(); err != nil {
		// TODO: make searches take a context, so they can be cancelled early
		conn.Close()
		return nil, err
	}
	conn.session.setConn(nil, conn)
	var cancel context.CancelFunc
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel = context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	select {
	case <-conn.session.init:
		return conn, nil
	case <-ctx.Done():
		conn.Close()
		return nil, errors.New("session handshake timeout")
	}
}
