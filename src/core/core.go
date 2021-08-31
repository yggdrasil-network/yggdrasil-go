package core

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"time"

	iwe "github.com/Arceliar/ironwood/encrypted"
	iwt "github.com/Arceliar/ironwood/types"
	"github.com/Arceliar/phony"
	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	//"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"
)

// The Core object represents the Yggdrasil node. You should create a Core
// object for each Yggdrasil node you plan to run.
type Core struct {
	// This is the main data structure that holds everything else for a node
	// We're going to keep our own copy of the provided config - that way we can
	// guarantee that it will be covered by the mutex
	phony.Inbox
	*iwe.PacketConn
	config       *config.NodeConfig // Config
	secret       ed25519.PrivateKey
	public       ed25519.PublicKey
	links        links
	proto        protoHandler
	log          *log.Logger
	addPeerTimer *time.Timer
	ctx          context.Context
	ctxCancel    context.CancelFunc
}

func (c *Core) _init() error {
	// TODO separate init and start functions
	//  Init sets up structs
	//  Start launches goroutines that depend on structs being set up
	// This is pretty much required to completely avoid race conditions
	c.config.RLock()
	defer c.config.RUnlock()
	if c.log == nil {
		c.log = log.New(ioutil.Discard, "", 0)
	}

	sigPriv, err := hex.DecodeString(c.config.PrivateKey)
	if err != nil {
		return err
	}
	if len(sigPriv) < ed25519.PrivateKeySize {
		return errors.New("PrivateKey is incorrect length")
	}

	c.secret = ed25519.PrivateKey(sigPriv)
	c.public = c.secret.Public().(ed25519.PublicKey)
	// TODO check public against current.PublicKey, error if they don't match

	c.PacketConn, err = iwe.NewPacketConn(c.secret)
	c.ctx, c.ctxCancel = context.WithCancel(context.Background())
	c.proto.init(c)
	if err := c.proto.nodeinfo.setNodeInfo(c.config.NodeInfo, c.config.NodeInfoPrivacy); err != nil {
		return fmt.Errorf("setNodeInfo: %w", err)
	}
	return err
}

// If any static peers were provided in the configuration above then we should
// configure them. The loop ensures that disconnected peers will eventually
// be reconnected with.
func (c *Core) _addPeerLoop() {
	c.config.RLock()
	defer c.config.RUnlock()

	if c.addPeerTimer == nil {
		return
	}

	// Add peers from the Peers section
	for _, peer := range c.config.Peers {
		go func(peer string, intf string) {
			u, err := url.Parse(peer)
			if err != nil {
				c.log.Errorln("Failed to parse peer url:", peer, err)
			}
			if err := c.CallPeer(u, intf); err != nil {
				c.log.Errorln("Failed to add peer:", err)
			}
		}(peer, "") // TODO: this should be acted and not in a goroutine?
	}

	// Add peers from the InterfacePeers section
	for intf, intfpeers := range c.config.InterfacePeers {
		for _, peer := range intfpeers {
			go func(peer string, intf string) {
				u, err := url.Parse(peer)
				if err != nil {
					c.log.Errorln("Failed to parse peer url:", peer, err)
				}
				if err := c.CallPeer(u, intf); err != nil {
					c.log.Errorln("Failed to add peer:", err)
				}
			}(peer, intf) // TODO: this should be acted and not in a goroutine?
		}
	}

	c.addPeerTimer = time.AfterFunc(time.Minute, func() {
		c.Act(nil, c._addPeerLoop)
	})
}

// Start starts up Yggdrasil using the provided config.NodeConfig, and outputs
// debug logging through the provided log.Logger. The started stack will include
// TCP and UDP sockets, a multicast discovery socket, an admin socket, router,
// switch and DHT node. A config.NodeState is returned which contains both the
// current and previous configurations (from reconfigures).
func (c *Core) Start(nc *config.NodeConfig, log *log.Logger) (err error) {
	phony.Block(c, func() {
		err = c._start(nc, log)
	})
	return
}

// This function is unsafe and should only be ran by the core actor.
func (c *Core) _start(nc *config.NodeConfig, log *log.Logger) error {
	c.log = log
	c.config = nc

	if name := version.BuildName(); name != "unknown" {
		c.log.Infoln("Build name:", name)
	}
	if version := version.BuildVersion(); version != "unknown" {
		c.log.Infoln("Build version:", version)
	}

	c.log.Infoln("Starting up...")
	if err := c._init(); err != nil {
		c.log.Errorln("Failed to initialize core")
		return err
	}

	if err := c.links.init(c); err != nil {
		c.log.Errorln("Failed to start link interfaces")
		return err
	}

	c.addPeerTimer = time.AfterFunc(0, func() {
		c.Act(nil, c._addPeerLoop)
	})

	c.log.Infoln("Startup complete")
	return nil
}

// Stop shuts down the Yggdrasil node.
func (c *Core) Stop() {
	phony.Block(c, func() {
		c.log.Infoln("Stopping...")
		c._close()
		c.log.Infoln("Stopped")
	})
}

func (c *Core) Close() error {
	var err error
	phony.Block(c, func() {
		err = c._close()
	})
	return err
}

// This function is unsafe and should only be ran by the core actor.
func (c *Core) _close() error {
	c.ctxCancel()
	err := c.PacketConn.Close()
	if c.addPeerTimer != nil {
		c.addPeerTimer.Stop()
		c.addPeerTimer = nil
	}
	_ = c.links.stop()
	return err
}

func (c *Core) MTU() uint64 {
	const sessionTypeOverhead = 1
	return c.PacketConn.MTU() - sessionTypeOverhead
}

func (c *Core) ReadFrom(p []byte) (n int, from net.Addr, err error) {
	buf := make([]byte, c.PacketConn.MTU(), 65535)
	for {
		bs := buf
		n, from, err = c.PacketConn.ReadFrom(bs)
		if err != nil {
			return 0, from, err
		}
		if n == 0 {
			continue
		}
		switch bs[0] {
		case typeSessionTraffic:
			// This is what we want to handle here
		case typeSessionProto:
			var key keyArray
			copy(key[:], from.(iwt.Addr))
			data := append([]byte(nil), bs[1:n]...)
			c.proto.handleProto(nil, key, data)
			continue
		default:
			continue
		}
		bs = bs[1:n]
		copy(p, bs)
		if len(p) < len(bs) {
			n = len(p)
		} else {
			n = len(bs)
		}
		return
	}
}

func (c *Core) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	buf := make([]byte, 0, 65535)
	buf = append(buf, typeSessionTraffic)
	buf = append(buf, p...)
	n, err = c.PacketConn.WriteTo(buf, addr)
	if n > 0 {
		n -= 1
	}
	return
}
