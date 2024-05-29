package core

import (
	"crypto/ed25519"
	"encoding/json"
	"net"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/Arceliar/phony"

	"github.com/Arceliar/ironwood/network"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type SelfInfo struct {
	Key            ed25519.PublicKey
	RoutingEntries uint64
}

type PeerInfo struct {
	URI           string
	Up            bool
	Inbound       bool
	LastError     error
	LastErrorTime time.Time
	Key           ed25519.PublicKey
	Root          ed25519.PublicKey
	Coords        []uint64
	Port          uint64
	Priority      uint8
	RXBytes       uint64
	TXBytes       uint64
	Uptime        time.Duration
	Latency       time.Duration
}

type TreeEntryInfo struct {
	Key      ed25519.PublicKey
	Parent   ed25519.PublicKey
	Sequence uint64
	//Port uint64
	//Rest uint64
}

type PathEntryInfo struct {
	Key      ed25519.PublicKey
	Path     []uint64
	Sequence uint64
}

type SessionInfo struct {
	Key     ed25519.PublicKey
	RXBytes uint64
	TXBytes uint64
	Uptime  time.Duration
}

func (c *Core) GetSelf() SelfInfo {
	var self SelfInfo
	s := c.PacketConn.PacketConn.Debug.GetSelf()
	self.Key = s.Key
	self.RoutingEntries = s.RoutingEntries
	return self
}

func (c *Core) GetPeers() []PeerInfo {
	peers := []PeerInfo{}
	conns := map[net.Conn]network.DebugPeerInfo{}
	iwpeers := c.PacketConn.PacketConn.Debug.GetPeers()
	for _, p := range iwpeers {
		conns[p.Conn] = p
	}

	phony.Block(&c.links, func() {
		for info, state := range c.links._links {
			var peerinfo PeerInfo
			var conn net.Conn
			peerinfo.URI = info.uri
			peerinfo.LastError = state._err
			peerinfo.LastErrorTime = state._errtime
			if c := state._conn; c != nil {
				conn = c
				peerinfo.Up = true
				peerinfo.Inbound = state.linkType == linkTypeIncoming
				peerinfo.RXBytes = atomic.LoadUint64(&c.rx)
				peerinfo.TXBytes = atomic.LoadUint64(&c.tx)
				peerinfo.Uptime = time.Since(c.up)
			}
			if p, ok := conns[conn]; ok {
				peerinfo.Key = p.Key
				peerinfo.Root = p.Root
				peerinfo.Port = p.Port
				peerinfo.Priority = p.Priority
				peerinfo.Latency = p.Latency
			}
			peers = append(peers, peerinfo)
		}
	})

	return peers
}

func (c *Core) GetTree() []TreeEntryInfo {
	var trees []TreeEntryInfo
	ts := c.PacketConn.PacketConn.Debug.GetTree()
	for _, t := range ts {
		var info TreeEntryInfo
		info.Key = t.Key
		info.Parent = t.Parent
		info.Sequence = t.Sequence
		//info.Port = d.Port
		//info.Rest = d.Rest
		trees = append(trees, info)
	}
	return trees
}

func (c *Core) GetPaths() []PathEntryInfo {
	var paths []PathEntryInfo
	ps := c.PacketConn.PacketConn.Debug.GetPaths()
	for _, p := range ps {
		var info PathEntryInfo
		info.Key = p.Key
		info.Sequence = p.Sequence
		info.Path = p.Path
		paths = append(paths, info)
	}
	return paths
}

func (c *Core) GetSessions() []SessionInfo {
	var sessions []SessionInfo
	ss := c.PacketConn.Debug.GetSessions()
	for _, s := range ss {
		var info SessionInfo
		info.Key = s.Key
		info.RXBytes = s.RX
		info.TXBytes = s.TX
		info.Uptime = s.Uptime
		sessions = append(sessions, info)
	}
	return sessions
}

// Listen starts a new listener (either TCP or TLS). The input should be a url.URL
// parsed from a string of the form e.g. "tcp://a.b.c.d:e". In the case of a
// link-local address, the interface should be provided as the second argument.
func (c *Core) Listen(u *url.URL, sintf string) (*Listener, error) {
	return c.links.listen(u, sintf)
}

// Address gets the IPv6 address of the Yggdrasil node. This is always a /128
// address. The IPv6 address is only relevant when the node is operating as an
// IP router and often is meaningless when embedded into an application, unless
// that application also implements either VPN functionality or deals with IP
// packets specifically.
func (c *Core) Address() net.IP {
	addr := net.IP(address.AddrForKey(c.public)[:])
	return addr
}

// Subnet gets the routed IPv6 subnet of the Yggdrasil node. This is always a
// /64 subnet. The IPv6 subnet is only relevant when the node is operating as an
// IP router and often is meaningless when embedded into an application, unless
// that application also implements either VPN functionality or deals with IP
// packets specifically.
func (c *Core) Subnet() net.IPNet {
	subnet := address.SubnetForKey(c.public)[:]
	subnet = append(subnet, 0, 0, 0, 0, 0, 0, 0, 0)
	return net.IPNet{IP: subnet, Mask: net.CIDRMask(64, 128)}
}

// SetLogger sets the output logger of the Yggdrasil node after startup. This
// may be useful if you want to redirect the output later. Note that this
// expects a Logger from the github.com/gologme/log package and not from Go's
// built-in log package.
func (c *Core) SetLogger(log Logger) {
	c.log = log
}

// AddPeer adds a peer. This should be specified in the peer URI format, e.g.:
//
//	tcp://a.b.c.d:e
//	socks://a.b.c.d:e/f.g.h.i:j
//
// This adds the peer to the peer list, so that they will be called again if the
// connection drops.
func (c *Core) AddPeer(u *url.URL, sintf string) error {
	return c.links.add(u, sintf, linkTypePersistent)
}

// RemovePeer removes a peer. The peer should be specified in URI format, see AddPeer.
// The peer is not disconnected immediately.
func (c *Core) RemovePeer(u *url.URL, sintf string) error {
	return c.links.remove(u, sintf, linkTypePersistent)
}

// CallPeer calls a peer once. This should be specified in the peer URI format,
// e.g.:
//
//	tcp://a.b.c.d:e
//	socks://a.b.c.d:e/f.g.h.i:j
//
// This does not add the peer to the peer list, so if the connection drops, the
// peer will not be called again automatically.
func (c *Core) CallPeer(u *url.URL, sintf string) error {
	return c.links.add(u, sintf, linkTypeEphemeral)
}

func (c *Core) PublicKey() ed25519.PublicKey {
	return c.public
}

// Hack to get the admin stuff working, TODO something cleaner

type AddHandler interface {
	AddHandler(name, desc string, args []string, handlerfunc AddHandlerFunc) error
}

type AddHandlerFunc func(json.RawMessage) (interface{}, error)

// SetAdmin must be called after Init and before Start.
// It sets the admin handler for NodeInfo and the Debug admin functions.
func (c *Core) SetAdmin(a AddHandler) error {
	if err := a.AddHandler(
		"getNodeInfo", "Request nodeinfo from a remote node by its public key", []string{"key"},
		c.proto.nodeinfo.nodeInfoAdminHandler,
	); err != nil {
		return err
	}
	if err := a.AddHandler(
		"debug_remoteGetSelf", "Debug use only", []string{"key"},
		c.proto.getSelfHandler,
	); err != nil {
		return err
	}
	if err := a.AddHandler(
		"debug_remoteGetPeers", "Debug use only", []string{"key"},
		c.proto.getPeersHandler,
	); err != nil {
		return err
	}
	if err := a.AddHandler(
		"debug_remoteGetTree", "Debug use only", []string{"key"},
		c.proto.getTreeHandler,
	); err != nil {
		return err
	}
	return nil
}
