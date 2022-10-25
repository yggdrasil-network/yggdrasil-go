package core

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/Arceliar/phony"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type SelfInfo struct {
	Key    ed25519.PublicKey
	Root   ed25519.PublicKey
	Coords []uint64
}

type PeerInfo struct {
	Key      ed25519.PublicKey
	Root     ed25519.PublicKey
	Coords   []uint64
	Port     uint64
	Priority uint8
	Remote   string
	RXBytes  uint64
	TXBytes  uint64
	Uptime   time.Duration
}

type DHTEntryInfo struct {
	Key  ed25519.PublicKey
	Port uint64
	Rest uint64
}

type PathEntryInfo struct {
	Key  ed25519.PublicKey
	Path []uint64
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
	self.Root = s.Root
	self.Coords = s.Coords
	return self
}

func (c *Core) GetPeers() []PeerInfo {
	var peers []PeerInfo
	names := make(map[net.Conn]string)
	phony.Block(&c.links, func() {
		for _, info := range c.links._links {
			if info == nil {
				continue
			}
			names[info.conn] = info.lname
		}
	})
	ps := c.PacketConn.PacketConn.Debug.GetPeers()
	for _, p := range ps {
		var info PeerInfo
		info.Key = p.Key
		info.Root = p.Root
		info.Coords = p.Coords
		info.Port = p.Port
		info.Priority = p.Priority
		info.Remote = p.Conn.RemoteAddr().String()
		if name := names[p.Conn]; name != "" {
			info.Remote = name
		}
		if linkconn, ok := p.Conn.(*linkConn); ok {
			info.RXBytes = atomic.LoadUint64(&linkconn.rx)
			info.TXBytes = atomic.LoadUint64(&linkconn.tx)
			info.Uptime = time.Since(linkconn.up)
		}
		peers = append(peers, info)
	}
	return peers
}

func (c *Core) GetDHT() []DHTEntryInfo {
	var dhts []DHTEntryInfo
	ds := c.PacketConn.PacketConn.Debug.GetDHT()
	for _, d := range ds {
		var info DHTEntryInfo
		info.Key = d.Key
		info.Port = d.Port
		info.Rest = d.Rest
		dhts = append(dhts, info)
	}
	return dhts
}

func (c *Core) GetPaths() []PathEntryInfo {
	var paths []PathEntryInfo
	ps := c.PacketConn.PacketConn.Debug.GetPaths()
	for _, p := range ps {
		var info PathEntryInfo
		info.Key = p.Key
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
	switch u.Scheme {
	case "tcp":
		return c.links.tcp.listen(u, sintf)
	case "tls":
		return c.links.tls.listen(u, sintf)
	case "unix":
		return c.links.unix.listen(u, sintf)
	default:
		return nil, fmt.Errorf("unrecognised scheme %q", u.Scheme)
	}
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
func (c *Core) AddPeer(uri string, sourceInterface string) error {
	var known bool
	phony.Block(c, func() {
		_, known = c.config._peers[Peer{uri, sourceInterface}]
	})
	if known {
		return fmt.Errorf("peer already configured")
	}
	u, err := url.Parse(uri)
	if err != nil {
		return err
	}
	info, err := c.links.call(u, sourceInterface)
	if err != nil {
		return err
	}
	phony.Block(c, func() {
		c.config._peers[Peer{uri, sourceInterface}] = &info
	})
	return nil
}

// RemovePeer removes a peer. The peer should be specified in URI format, see AddPeer.
// The peer is not disconnected immediately.
func (c *Core) RemovePeer(uri string, sourceInterface string) error {
	var err error
	phony.Block(c, func() {
		peer := Peer{uri, sourceInterface}
		linkInfo, ok := c.config._peers[peer]
		if !ok {
			err = fmt.Errorf("peer not configured")
			return
		}
		if ok && linkInfo != nil {
			c.links.Act(nil, func() {
				if link := c.links._links[*linkInfo]; link != nil {
					_ = link.close()
				}
			})
		}
		delete(c.config._peers, peer)
	})
	return err
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
	_, err := c.links.call(u, sintf)
	return err
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
		"debug_remoteGetDHT", "Debug use only", []string{"key"},
		c.proto.getDHTHandler,
	); err != nil {
		return err
	}
	return nil
}
