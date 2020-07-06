package yggdrasil

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/gologme/log"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"

	"github.com/Arceliar/phony"
)

// Peer represents a single peer object. This contains information from the
// preferred switch port for this peer, although there may be more than one
// active switch port connection to the peer in reality.
//
// This struct is informational only - you cannot manipulate peer connections
// using instances of this struct. You should use the AddPeer or RemovePeer
// functions instead.
type Peer struct {
	PublicKey  crypto.BoxPubKey // The public key of the remote node
	Endpoint   string           // The connection string used to connect to the peer
	BytesSent  uint64           // Number of bytes sent to this peer
	BytesRecvd uint64           // Number of bytes received from this peer
	Protocol   string           // The transport protocol that this peer is connected with, typically "tcp"
	Port       uint64           // Switch port number for this peer connection
	Uptime     time.Duration    // How long this peering has been active for
}

// SwitchPeer represents a switch connection to a peer. Note that there may be
// multiple switch peers per actual peer, e.g. if there are multiple connections
// to a given node.
//
// This struct is informational only - you cannot manipulate switch peer
// connections using instances of this struct. You should use the AddPeer or
// RemovePeer functions instead.
type SwitchPeer struct {
	PublicKey  crypto.BoxPubKey // The public key of the remote node
	Coords     []uint64         // The coordinates of the remote node
	BytesSent  uint64           // Number of bytes sent via this switch port
	BytesRecvd uint64           // Number of bytes received via this switch port
	Port       uint64           // Switch port number for this switch peer
	Protocol   string           // The transport protocol that this switch port is connected with, typically "tcp"
	Endpoint   string           // The connection string used to connect to the switch peer
}

// DHTEntry represents a single DHT entry that has been learned or cached from
// DHT searches.
type DHTEntry struct {
	PublicKey crypto.BoxPubKey
	Coords    []uint64
	LastSeen  time.Duration
}

// DHTRes represents a DHT response, as returned by DHTPing.
type DHTRes struct {
	PublicKey crypto.BoxPubKey // key of the sender
	Coords    []uint64         // coords of the sender
	Dest      crypto.NodeID    // the destination node ID
	Infos     []DHTEntry       // response
}

// NodeInfoPayload represents a RequestNodeInfo response, in bytes.
type NodeInfoPayload []byte

// SwitchQueues represents information from the switch related to link
// congestion and a list of switch queues created in response to congestion on a
// given link.
type SwitchQueues struct {
	Queues       []SwitchQueue // An array of SwitchQueue objects containing information about individual queues
	Count        uint64        // The current number of active switch queues
	Size         uint64        // The current total size of active switch queues
	HighestCount uint64        // The highest recorded number of switch queues so far
	HighestSize  uint64        // The highest recorded total size of switch queues so far
	MaximumSize  uint64        // The maximum allowed total size of switch queues, as specified by config
}

// SwitchQueue represents a single switch queue. Switch queues are only created
// in response to congestion on a given link and represent how much data has
// been temporarily cached for sending once the congestion has cleared.
type SwitchQueue struct {
	ID      string // The ID of the switch queue
	Size    uint64 // The total size, in bytes, of the queue
	Packets uint64 // The number of packets in the queue
	Port    uint64 // The switch port to which the queue applies
}

// Session represents an open session with another node. Sessions are opened in
// response to traffic being exchanged between two nodes using Conn objects.
// Note that sessions will automatically be closed by Yggdrasil if no traffic is
// exchanged for around two minutes.
type Session struct {
	PublicKey   crypto.BoxPubKey // The public key of the remote node
	Coords      []uint64         // The coordinates of the remote node
	BytesSent   uint64           // Bytes sent to the session
	BytesRecvd  uint64           // Bytes received from the session
	MTU         MTU              // The maximum supported message size of the session
	Uptime      time.Duration    // How long this session has been active for
	WasMTUFixed bool             // This field is no longer used
}

// GetPeers returns one or more Peer objects containing information about active
// peerings with other Yggdrasil nodes, where one of the responses always
// includes information about the current node (with a port number of 0). If
// there is exactly one entry then this node is not connected to any other nodes
// and is therefore isolated.
func (c *Core) GetPeers() []Peer {
	var ports map[switchPort]*peer
	phony.Block(&c.peers, func() { ports = c.peers.ports })
	var peers []Peer
	var ps []switchPort
	for port := range ports {
		ps = append(ps, port)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i] < ps[j] })
	for _, port := range ps {
		p := ports[port]
		var info Peer
		phony.Block(p, func() {
			info = Peer{
				Endpoint:   p.intf.name(),
				BytesSent:  p.bytesSent,
				BytesRecvd: p.bytesRecvd,
				Protocol:   p.intf.interfaceType(),
				Port:       uint64(port),
				Uptime:     time.Since(p.firstSeen),
			}
			copy(info.PublicKey[:], p.box[:])
		})
		peers = append(peers, info)
	}
	return peers
}

// GetSwitchPeers returns zero or more SwitchPeer objects containing information
// about switch port connections with other Yggdrasil nodes. Note that, unlike
// GetPeers, GetSwitchPeers does not include information about the current node,
// therefore it is possible for this to return zero elements if the node is
// isolated or not connected to any peers.
func (c *Core) GetSwitchPeers() []SwitchPeer {
	var switchpeers []SwitchPeer
	var table *lookupTable
	var ports map[switchPort]*peer
	phony.Block(&c.peers, func() {
		table = c.peers.table
		ports = c.peers.ports
	})
	for _, elem := range table.elems {
		peer, isIn := ports[elem.port]
		if !isIn {
			continue
		}
		coords := elem.locator.getCoords()
		var info SwitchPeer
		phony.Block(peer, func() {
			info = SwitchPeer{
				Coords:     append([]uint64{}, wire_coordsBytestoUint64s(coords)...),
				BytesSent:  peer.bytesSent,
				BytesRecvd: peer.bytesRecvd,
				Port:       uint64(elem.port),
				Protocol:   peer.intf.interfaceType(),
				Endpoint:   peer.intf.remote(),
			}
			copy(info.PublicKey[:], peer.box[:])
		})
		switchpeers = append(switchpeers, info)
	}
	return switchpeers
}

// GetDHT returns zero or more entries as stored in the DHT, cached primarily
// from searches that have already taken place.
func (c *Core) GetDHT() []DHTEntry {
	var dhtentries []DHTEntry
	getDHT := func() {
		now := time.Now()
		var dhtentry []*dhtInfo
		for _, v := range c.router.dht.table {
			dhtentry = append(dhtentry, v)
		}
		sort.SliceStable(dhtentry, func(i, j int) bool {
			return dht_ordered(&c.router.dht.nodeID, dhtentry[i].getNodeID(), dhtentry[j].getNodeID())
		})
		for _, v := range dhtentry {
			info := DHTEntry{
				Coords:   append([]uint64{}, wire_coordsBytestoUint64s(v.coords)...),
				LastSeen: now.Sub(v.recv),
			}
			copy(info.PublicKey[:], v.key[:])
			dhtentries = append(dhtentries, info)
		}
	}
	phony.Block(&c.router, getDHT)
	return dhtentries
}

// GetSessions returns a list of open sessions from this node to other nodes.
func (c *Core) GetSessions() []Session {
	var sessions []Session
	getSessions := func() {
		for _, sinfo := range c.router.sessions.sinfos {
			var session Session
			workerFunc := func() {
				session = Session{
					Coords:      append([]uint64{}, wire_coordsBytestoUint64s(sinfo.coords)...),
					MTU:         sinfo._getMTU(),
					BytesSent:   sinfo.bytesSent,
					BytesRecvd:  sinfo.bytesRecvd,
					Uptime:      time.Now().Sub(sinfo.timeOpened),
					WasMTUFixed: sinfo.wasMTUFixed,
				}
				copy(session.PublicKey[:], sinfo.theirPermPub[:])
			}
			phony.Block(sinfo, workerFunc)
			// TODO? skipped known but timed out sessions?
			sessions = append(sessions, session)
		}
	}
	phony.Block(&c.router, getSessions)
	return sessions
}

// ConnListen returns a listener for Yggdrasil session connections. You can only
// call this function once as each Yggdrasil node can only have a single
// ConnListener. Make sure to keep the reference to this for as long as it is
// needed.
func (c *Core) ConnListen() (*Listener, error) {
	c.router.sessions.listenerMutex.Lock()
	defer c.router.sessions.listenerMutex.Unlock()
	if c.router.sessions.listener != nil {
		return nil, errors.New("a listener already exists")
	}
	c.router.sessions.listener = &Listener{
		core:  c,
		conn:  make(chan *Conn),
		close: make(chan interface{}),
	}
	return c.router.sessions.listener, nil
}

// ConnDialer returns a dialer for Yggdrasil session connections. Since
// ConnDialers are stateless, you can request as many dialers as you like,
// although ideally you should request only one and keep the reference to it for
// as long as it is needed.
func (c *Core) ConnDialer() (*Dialer, error) {
	return &Dialer{
		core: c,
	}, nil
}

// ListenTCP starts a new TCP listener. The input URI should match that of the
// "Listen" configuration item, e.g.
// 		tcp://a.b.c.d:e
func (c *Core) ListenTCP(uri string) (*TcpListener, error) {
	return c.links.tcp.listen(uri, nil)
}

// ListenTLS starts a new TLS listener. The input URI should match that of the
// "Listen" configuration item, e.g.
// 		tls://a.b.c.d:e
func (c *Core) ListenTLS(uri string) (*TcpListener, error) {
	return c.links.tcp.listen(uri, c.links.tcp.tls.forListener)
}

// NodeID gets the node ID. This is derived from your router encryption keys.
// Remote nodes wanting to open connections to your node will need to know your
// node ID.
func (c *Core) NodeID() *crypto.NodeID {
	return crypto.GetNodeID(&c.boxPub)
}

// TreeID gets the tree ID. This is derived from your switch signing keys. There
// is typically no need to share this key.
func (c *Core) TreeID() *crypto.TreeID {
	return crypto.GetTreeID(&c.sigPub)
}

// SigningPublicKey gets the node's signing public key, as used by the switch.
func (c *Core) SigningPublicKey() string {
	return hex.EncodeToString(c.sigPub[:])
}

// EncryptionPublicKey gets the node's encryption public key, as used by the
// router.
func (c *Core) EncryptionPublicKey() string {
	return hex.EncodeToString(c.boxPub[:])
}

// Coords returns the current coordinates of the node. Note that these can
// change at any time for a number of reasons, not limited to but including
// changes to peerings (either yours or a parent nodes) or changes to the network
// root.
//
// This function may return an empty array - this is normal behaviour if either
// you are the root of the network that you are connected to, or you are not
// connected to any other nodes (effectively making you the root of a
// single-node network).
func (c *Core) Coords() []uint64 {
	var coords []byte
	phony.Block(&c.router, func() {
		coords = c.router.table.self.getCoords()
	})
	return wire_coordsBytestoUint64s(coords)
}

// Address gets the IPv6 address of the Yggdrasil node. This is always a /128
// address. The IPv6 address is only relevant when the node is operating as an
// IP router and often is meaningless when embedded into an application, unless
// that application also implements either VPN functionality or deals with IP
// packets specifically.
func (c *Core) Address() net.IP {
	address := net.IP(address.AddrForNodeID(c.NodeID())[:])
	return address
}

// Subnet gets the routed IPv6 subnet of the Yggdrasil node. This is always a
// /64 subnet. The IPv6 subnet is only relevant when the node is operating as an
// IP router and often is meaningless when embedded into an application, unless
// that application also implements either VPN functionality or deals with IP
// packets specifically.
func (c *Core) Subnet() net.IPNet {
	subnet := address.SubnetForNodeID(c.NodeID())[:]
	subnet = append(subnet, 0, 0, 0, 0, 0, 0, 0, 0)
	return net.IPNet{IP: subnet, Mask: net.CIDRMask(64, 128)}
}

// MyNodeInfo gets the currently configured nodeinfo. NodeInfo is typically
// specified through the "NodeInfo" option in the node configuration or using
// the SetNodeInfo function, although it may also contain other built-in values
// such as "buildname", "buildversion" etc.
func (c *Core) MyNodeInfo() NodeInfoPayload {
	return c.router.nodeinfo.getNodeInfo()
}

// SetNodeInfo sets the local nodeinfo. Note that nodeinfo can be any value or
// struct, it will be serialised into JSON automatically.
func (c *Core) SetNodeInfo(nodeinfo interface{}, nodeinfoprivacy bool) {
	c.router.nodeinfo.setNodeInfo(nodeinfo, nodeinfoprivacy)
}

// GetMaximumSessionMTU returns the maximum allowed session MTU size.
func (c *Core) GetMaximumSessionMTU() MTU {
	var mtu MTU
	phony.Block(&c.router, func() {
		mtu = c.router.sessions.myMaximumMTU
	})
	return mtu
}

// SetMaximumSessionMTU sets the maximum allowed session MTU size. The default
// value is 65535 bytes. Session pings will be sent to update all open sessions
// if the MTU has changed.
func (c *Core) SetMaximumSessionMTU(mtu MTU) {
	phony.Block(&c.router, func() {
		if c.router.sessions.myMaximumMTU != mtu {
			c.router.sessions.myMaximumMTU = mtu
			c.router.sessions.reconfigure()
		}
	})
}

// GetNodeInfo requests nodeinfo from a remote node, as specified by the public
// key and coordinates specified. The third parameter specifies whether a cached
// result is acceptable - this results in less traffic being generated than is
// necessary when, e.g. crawling the network.
func (c *Core) GetNodeInfo(key crypto.BoxPubKey, coords []uint64, nocache bool) (NodeInfoPayload, error) {
	response := make(chan *NodeInfoPayload, 1)
	c.router.nodeinfo.addCallback(key, func(nodeinfo *NodeInfoPayload) {
		defer func() { recover() }()
		select {
		case response <- nodeinfo:
		default:
		}
	})
	c.router.nodeinfo.sendNodeInfo(key, wire_coordsUint64stoBytes(coords), false)
	phony.Block(&c.router.nodeinfo, func() {}) // Wait for sendNodeInfo before starting timer
	timer := time.AfterFunc(6*time.Second, func() { close(response) })
	defer timer.Stop()
	for res := range response {
		return *res, nil
	}
	return NodeInfoPayload{}, fmt.Errorf("getNodeInfo timeout: %s", hex.EncodeToString(key[:]))
}

// SetSessionGatekeeper allows you to configure a handler function for deciding
// whether a session should be allowed or not. The default session firewall is
// implemented in this way. The function receives the public key of the remote
// side and a boolean which is true if we initiated the session or false if we
// received an incoming session request. The function should return true to
// allow the session or false to reject it.
func (c *Core) SetSessionGatekeeper(f func(pubkey *crypto.BoxPubKey, initiator bool) bool) {
	c.router.sessions.isAllowedMutex.Lock()
	defer c.router.sessions.isAllowedMutex.Unlock()

	c.router.sessions.isAllowedHandler = f
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

// CallPeer calls a peer once. This should be specified in the peer URI format,
// e.g.:
// 		tcp://a.b.c.d:e
//		socks://a.b.c.d:e/f.g.h.i:j
// This does not add the peer to the peer list, so if the connection drops, the
// peer will not be called again automatically.
func (c *Core) CallPeer(addr string, sintf string) error {
	return c.links.call(addr, sintf)
}

// DisconnectPeer disconnects a peer once. This should be specified as a port
// number.
func (c *Core) DisconnectPeer(port uint64) error {
	c.peers.Act(nil, func() {
		if p, isIn := c.peers.ports[switchPort(port)]; isIn {
			p.Act(&c.peers, p._removeSelf)
		}
	})
	return nil
}

// GetAllowedEncryptionPublicKeys returns the public keys permitted for incoming
// peer connections. If this list is empty then all incoming peer connections
// are accepted by default.
func (c *Core) GetAllowedEncryptionPublicKeys() []string {
	return c.peers.getAllowedEncryptionPublicKeys()
}

// AddAllowedEncryptionPublicKey whitelists a key for incoming peer connections.
// By default all incoming peer connections are accepted, but adding public keys
// to the whitelist using this function enables strict checking from that point
// forward. Once the whitelist is enabled, only peer connections from
// whitelisted public keys will be accepted.
func (c *Core) AddAllowedEncryptionPublicKey(bstr string) (err error) {
	c.peers.addAllowedEncryptionPublicKey(bstr)
	return nil
}

// RemoveAllowedEncryptionPublicKey removes a key from the whitelist for
// incoming peer connections. If none are set, an empty list permits all
// incoming connections.
func (c *Core) RemoveAllowedEncryptionPublicKey(bstr string) (err error) {
	c.peers.removeAllowedEncryptionPublicKey(bstr)
	return nil
}

// DHTPing sends a DHT ping to the node with the provided key and coords,
// optionally looking up the specified target NodeID.
func (c *Core) DHTPing(key crypto.BoxPubKey, coords []uint64, target *crypto.NodeID) (DHTRes, error) {
	resCh := make(chan *dhtRes, 1)
	info := dhtInfo{
		key:    key,
		coords: wire_coordsUint64stoBytes(coords),
	}
	if target == nil {
		target = info.getNodeID()
	}
	rq := dhtReqKey{info.key, *target}
	sendPing := func() {
		c.router.dht.addCallback(&rq, func(res *dhtRes) {
			resCh <- res
		})
		c.router.dht.ping(&info, &rq.dest)
	}
	phony.Block(&c.router, sendPing)
	// TODO: do something better than the below...
	res := <-resCh
	if res != nil {
		r := DHTRes{
			Coords: append([]uint64{}, wire_coordsBytestoUint64s(res.Coords)...),
		}
		copy(r.PublicKey[:], res.Key[:])
		for _, i := range res.Infos {
			e := DHTEntry{
				Coords: append([]uint64{}, wire_coordsBytestoUint64s(i.coords)...),
			}
			copy(e.PublicKey[:], i.key[:])
			r.Infos = append(r.Infos, e)
		}
		return r, nil
	}
	return DHTRes{}, fmt.Errorf("DHT ping timeout: %s", hex.EncodeToString(key[:]))
}
