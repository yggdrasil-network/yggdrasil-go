package core

import (
	"crypto/ed25519"
	"sync/atomic"
	"time"

	//"encoding/hex"
	"encoding/json"
	//"errors"
	//"fmt"
	"net"
	"net/url"

	//"sort"
	//"time"

	"github.com/gologme/log"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	//"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	//"github.com/Arceliar/phony"
)

type Self struct {
	Key    ed25519.PublicKey
	Root   ed25519.PublicKey
	Coords []uint64
}

type Peer struct {
	Key     ed25519.PublicKey
	Root    ed25519.PublicKey
	Coords  []uint64
	Port    uint64
	Remote  string
	RXBytes uint64
	TXBytes uint64
	Uptime  time.Duration
}

type DHTEntry struct {
	Key  ed25519.PublicKey
	Port uint64
	Rest uint64
}

type PathEntry struct {
	Key  ed25519.PublicKey
	Path []uint64
}

type Session struct {
	Key ed25519.PublicKey
}

func (c *Core) GetSelf() Self {
	var self Self
	s := c.PacketConn.PacketConn.Debug.GetSelf()
	self.Key = s.Key
	self.Root = s.Root
	self.Coords = s.Coords
	return self
}

func (c *Core) GetPeers() []Peer {
	var peers []Peer
	names := make(map[net.Conn]string)
	c.links.mutex.Lock()
	for _, info := range c.links.links {
		names[info.conn] = info.lname
	}
	c.links.mutex.Unlock()
	ps := c.PacketConn.PacketConn.Debug.GetPeers()
	for _, p := range ps {
		var info Peer
		info.Key = p.Key
		info.Root = p.Root
		info.Coords = p.Coords
		info.Port = p.Port
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

func (c *Core) GetDHT() []DHTEntry {
	var dhts []DHTEntry
	ds := c.PacketConn.PacketConn.Debug.GetDHT()
	for _, d := range ds {
		var info DHTEntry
		info.Key = d.Key
		info.Port = d.Port
		info.Rest = d.Rest
		dhts = append(dhts, info)
	}
	return dhts
}

func (c *Core) GetPaths() []PathEntry {
	var paths []PathEntry
	ps := c.PacketConn.PacketConn.Debug.GetPaths()
	for _, p := range ps {
		var info PathEntry
		info.Key = p.Key
		info.Path = p.Path
		paths = append(paths, info)
	}
	return paths
}

func (c *Core) GetSessions() []Session {
	var sessions []Session
	ss := c.PacketConn.Debug.GetSessions()
	for _, s := range ss {
		var info Session
		info.Key = s.Key
		sessions = append(sessions, info)
	}
	return sessions
}

// Listen starts a new listener (either TCP or TLS). The input should be a url.URL
// parsed from a string of the form e.g. "tcp://a.b.c.d:e". In the case of a
// link-local address, the interface should be provided as the second argument.
func (c *Core) Listen(u *url.URL, sintf string) (*TcpListener, error) {
	return c.links.tcp.listenURL(u, sintf)
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
func (c *Core) SetLogger(log *log.Logger) {
	c.log = log
}

// AddPeer adds a peer. This should be specified in the peer URI format, e.g.:
// 		tcp://a.b.c.d:e
//		socks://a.b.c.d:e/f.g.h.i:j
// This adds the peer to the peer list, so that they will be called again if the
// connection drops.
/*
func (c *Core) AddPeer(addr string, sintf string) error {
	if err := c.CallPeer(addr, sintf); err != nil {
		// TODO: We maybe want this to write the peer to the persistent
		// configuration even if a connection attempt fails, but first we'll need to
		// move the code to check the peer URI so that we don't deliberately save a
		// peer with a known bad URI. Loading peers from config should really do the
		// same thing too but I don't think that happens today
		return err
	}
	c.config.Mutex.Lock()
	defer c.config.Mutex.Unlock()
	if sintf == "" {
		for _, peer := range c.config.Current.Peers {
			if peer == addr {
				return errors.New("peer already added")
			}
		}
		c.config.Current.Peers = append(c.config.Current.Peers, addr)
	} else {
		if _, ok := c.config.Current.InterfacePeers[sintf]; ok {
			for _, peer := range c.config.Current.InterfacePeers[sintf] {
				if peer == addr {
					return errors.New("peer already added")
				}
			}
		}
		if _, ok := c.config.Current.InterfacePeers[sintf]; !ok {
			c.config.Current.InterfacePeers[sintf] = []string{addr}
		} else {
			c.config.Current.InterfacePeers[sintf] = append(c.config.Current.InterfacePeers[sintf], addr)
		}
	}
	return nil
}
*/

/*
func (c *Core) RemovePeer(addr string, sintf string) error {
	if sintf == "" {
		for i, peer := range c.config.Current.Peers {
			if peer == addr {
				c.config.Current.Peers = append(c.config.Current.Peers[:i], c.config.Current.Peers[i+1:]...)
				break
			}
		}
	} else if _, ok := c.config.Current.InterfacePeers[sintf]; ok {
		for i, peer := range c.config.Current.InterfacePeers[sintf] {
			if peer == addr {
				c.config.Current.InterfacePeers[sintf] = append(c.config.Current.InterfacePeers[sintf][:i], c.config.Current.InterfacePeers[sintf][i+1:]...)
				break
			}
		}
	}

	panic("TODO") // Get the net.Conn to this peer (if any) and close it
	c.peers.Act(nil, func() {
		ports := c.peers.ports
		for _, peer := range ports {
			if addr == peer.intf.name() {
				c.peers._removePeer(peer)
			}
		}
	})

	return nil
}
*/

// CallPeer calls a peer once. This should be specified in the peer URI format,
// e.g.:
// 		tcp://a.b.c.d:e
//		socks://a.b.c.d:e/f.g.h.i:j
// This does not add the peer to the peer list, so if the connection drops, the
// peer will not be called again automatically.
func (c *Core) CallPeer(u *url.URL, sintf string) error {
	return c.links.call(u, sintf)
}

func (c *Core) PublicKey() ed25519.PublicKey {
	return c.public
}

// Hack to get the admin stuff working, TODO something cleaner

type AddHandler interface {
	AddHandler(name string, args []string, handlerfunc AddHandlerFunc) error
}

type AddHandlerFunc func(json.RawMessage) (interface{}, error)

// SetAdmin must be called after Init and before Start.
// It sets the admin handler for NodeInfo and the Debug admin functions.
func (c *Core) SetAdmin(a AddHandler) error {
	if err := a.AddHandler("getNodeInfo", []string{"key"}, c.proto.nodeinfo.nodeInfoAdminHandler); err != nil {
		return err
	}
	if err := a.AddHandler("debug_remoteGetSelf", []string{"key"}, c.proto.getSelfHandler); err != nil {
		return err
	}
	if err := a.AddHandler("debug_remoteGetPeers", []string{"key"}, c.proto.getPeersHandler); err != nil {
		return err
	}
	if err := a.AddHandler("debug_remoteGetDHT", []string{"key"}, c.proto.getDHTHandler); err != nil {
		return err
	}
	return nil
}
