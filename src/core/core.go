package core

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"time"

	iwe "github.com/Arceliar/ironwood/encrypted"
	iwt "github.com/Arceliar/ironwood/types"
	"github.com/Arceliar/phony"
	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/version"
	//"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

// The Core object represents the Yggdrasil node. You should create a Core
// object for each Yggdrasil node you plan to run.
type Core struct {
	// This is the main data structure that holds everything else for a node
	// We're going to keep our own copy of the provided config - that way we can
	// guarantee that it will be covered by the mutex
	phony.Inbox
	*iwe.PacketConn
	ctx          context.Context
	cancel       context.CancelFunc
	secret       ed25519.PrivateKey
	public       ed25519.PublicKey
	links        links
	proto        protoHandler
	log          *log.Logger
	addPeerTimer *time.Timer
	config       struct {
		_peers             map[Peer]struct{}          // configurable after startup
		_listeners         map[ListenAddress]struct{} // configurable after startup
		nodeinfo           NodeInfo                   // immutable after startup
		nodeinfoPrivacy    NodeInfoPrivacy            // immutable after startup
		ifname             IfName                     // immutable after startup
		ifmtu              IfMTU                      // immutable after startup
		_allowedPublicKeys map[[32]byte]struct{}      // configurable after startup
	}
}

func New(secret ed25519.PrivateKey, opts ...SetupOption) (*Core, error) {
	if len(secret) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key is incorrect length")
	}
	c := &Core{
		secret: secret,
		public: secret.Public().(ed25519.PublicKey),
		log:    log.New(os.Stdout, "", 0), // TODO: not this
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	var err error
	if c.PacketConn, err = iwe.NewPacketConn(c.secret); err != nil {
		return nil, fmt.Errorf("error creating encryption: %w", err)
	}
	c.config._peers = map[Peer]struct{}{}
	c.config._listeners = map[ListenAddress]struct{}{}
	c.config._allowedPublicKeys = map[[32]byte]struct{}{}
	for _, opt := range opts {
		c._applyOption(opt)
	}
	if c.log == nil {
		c.log = log.New(io.Discard, "", 0)
	}
	c.proto.init(c)
	if err := c.links.init(c); err != nil {
		return nil, fmt.Errorf("error initialising links: %w", err)
	}
	if err := c.proto.nodeinfo.setNodeInfo(c.config.nodeinfo, bool(c.config.nodeinfoPrivacy)); err != nil {
		return nil, fmt.Errorf("error setting node info: %w", err)
	}
	c.addPeerTimer = time.AfterFunc(time.Minute, func() {
		c.Act(nil, c._addPeerLoop)
	})
	if name := version.BuildName(); name != "unknown" {
		c.log.Infoln("Build name:", name)
	}
	if version := version.BuildVersion(); version != "unknown" {
		c.log.Infoln("Build version:", version)
	}
	return c, nil
}

func (c *Core) _applyOption(opt SetupOption) {
	switch v := opt.(type) {
	case Peer:
		c.config._peers[v] = struct{}{}
	case ListenAddress:
		c.config._listeners[v] = struct{}{}
	case NodeInfo:
		c.config.nodeinfo = v
	case NodeInfoPrivacy:
		c.config.nodeinfoPrivacy = v
	case IfName:
		c.config.ifname = v
	case IfMTU:
		c.config.ifmtu = v
	case AllowedPublicKey:
		pk := [32]byte{}
		copy(pk[:], v)
		c.config._allowedPublicKeys[pk] = struct{}{}
	}
}

// If any static peers were provided in the configuration above then we should
// configure them. The loop ensures that disconnected peers will eventually
// be reconnected with.
func (c *Core) _addPeerLoop() {
	if c.addPeerTimer == nil {
		return
	}

	// Add peers from the Peers section
	for peer := range c.config._peers {
		go func(peer string, intf string) {
			u, err := url.Parse(peer)
			if err != nil {
				c.log.Errorln("Failed to parse peer url:", peer, err)
			}
			if err := c.CallPeer(u, intf); err != nil {
				c.log.Errorln("Failed to add peer:", err)
			}
		}(peer.URI, peer.SourceInterface) // TODO: this should be acted and not in a goroutine?
	}

	c.addPeerTimer = time.AfterFunc(time.Minute, func() {
		c.Act(nil, c._addPeerLoop)
	})
}

// Stop shuts down the Yggdrasil node.
func (c *Core) Stop() {
	phony.Block(c, func() {
		c.log.Infoln("Stopping...")
		c._close()
		c.log.Infoln("Stopped")
	})
}

// This function is unsafe and should only be ran by the core actor.
func (c *Core) _close() error {
	c.cancel()
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
