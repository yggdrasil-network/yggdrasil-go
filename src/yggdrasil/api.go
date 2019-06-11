package yggdrasil

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gologme/log"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

// Peer represents a single peer object. This contains information from the
// preferred switch port for this peer, although there may be more than one in
// reality.
type Peer struct {
	PublicKey  crypto.BoxPubKey
	Endpoint   string
	BytesSent  uint64
	BytesRecvd uint64
	Protocol   string
	Port       uint64
	Uptime     time.Duration
}

// SwitchPeer represents a switch connection to a peer. Note that there may be
// multiple switch peers per actual peer, e.g. if there are multiple connections
// to a given node.
type SwitchPeer struct {
	PublicKey  crypto.BoxPubKey
	Coords     []byte
	BytesSent  uint64
	BytesRecvd uint64
	Port       uint64
	Protocol   string
	Endpoint   string
}

// DHTEntry represents a single DHT entry that has been learned or cached from
// DHT searches.
type DHTEntry struct {
	PublicKey crypto.BoxPubKey
	Coords    []byte
	LastSeen  time.Duration
}

// DHTRes represents a DHT response, as returned by DHTPing.
type DHTRes struct {
	PublicKey crypto.BoxPubKey // key of the sender
	Coords    []byte           // coords of the sender
	Dest      crypto.NodeID    // the destination node ID
	Infos     []DHTEntry       // response
}

// NodeInfoPayload represents a RequestNodeInfo response, in bytes.
type NodeInfoPayload nodeinfoPayload

// SwitchQueues represents information from the switch related to link
// congestion and a list of switch queues created in response to congestion on a
// given link.
type SwitchQueues struct {
	Queues       []SwitchQueue
	Count        uint64
	Size         uint64
	HighestCount uint64
	HighestSize  uint64
	MaximumSize  uint64
}

// SwitchQueue represents a single switch queue, which is created in response
// to congestion on a given link.
type SwitchQueue struct {
	ID      string
	Size    uint64
	Packets uint64
	Port    uint64
}

// Session represents an open session with another node.
type Session struct {
	PublicKey   crypto.BoxPubKey
	Coords      []byte
	BytesSent   uint64
	BytesRecvd  uint64
	MTU         uint16
	Uptime      time.Duration
	WasMTUFixed bool
}

// GetPeers returns one or more Peer objects containing information about active
// peerings with other Yggdrasil nodes, where one of the responses always
// includes information about the current node (with a port number of 0). If
// there is exactly one entry then this node is not connected to any other nodes
// and is therefore isolated.
func (c *Core) GetPeers() []Peer {
	ports := c.peers.ports.Load().(map[switchPort]*peer)
	var peers []Peer
	var ps []switchPort
	for port := range ports {
		ps = append(ps, port)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i] < ps[j] })
	for _, port := range ps {
		p := ports[port]
		info := Peer{
			Endpoint:   p.intf.name,
			BytesSent:  atomic.LoadUint64(&p.bytesSent),
			BytesRecvd: atomic.LoadUint64(&p.bytesRecvd),
			Protocol:   p.intf.info.linkType,
			Port:       uint64(port),
			Uptime:     time.Since(p.firstSeen),
		}
		copy(info.PublicKey[:], p.box[:])
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
	table := c.switchTable.table.Load().(lookupTable)
	peers := c.peers.ports.Load().(map[switchPort]*peer)
	for _, elem := range table.elems {
		peer, isIn := peers[elem.port]
		if !isIn {
			continue
		}
		coords := elem.locator.getCoords()
		info := SwitchPeer{
			Coords:     append([]byte{}, coords...),
			BytesSent:  atomic.LoadUint64(&peer.bytesSent),
			BytesRecvd: atomic.LoadUint64(&peer.bytesRecvd),
			Port:       uint64(elem.port),
			Protocol:   peer.intf.info.linkType,
			Endpoint:   peer.intf.info.remote,
		}
		copy(info.PublicKey[:], peer.box[:])
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
		for _, v := range c.dht.table {
			dhtentry = append(dhtentry, v)
		}
		sort.SliceStable(dhtentry, func(i, j int) bool {
			return dht_ordered(&c.dht.nodeID, dhtentry[i].getNodeID(), dhtentry[j].getNodeID())
		})
		for _, v := range dhtentry {
			info := DHTEntry{
				Coords:   append([]byte{}, v.coords...),
				LastSeen: now.Sub(v.recv),
			}
			copy(info.PublicKey[:], v.key[:])
			dhtentries = append(dhtentries, info)
		}
	}
	c.router.doAdmin(getDHT)
	return dhtentries
}

// GetSwitchQueues returns information about the switch queues that are
// currently in effect. These values can change within an instant.
func (c *Core) GetSwitchQueues() SwitchQueues {
	var switchqueues SwitchQueues
	switchTable := &c.switchTable
	getSwitchQueues := func() {
		switchqueues = SwitchQueues{
			Count:        uint64(len(switchTable.queues.bufs)),
			Size:         switchTable.queues.size,
			HighestCount: uint64(switchTable.queues.maxbufs),
			HighestSize:  switchTable.queues.maxsize,
			MaximumSize:  switchTable.queueTotalMaxSize,
		}
		for k, v := range switchTable.queues.bufs {
			nexthop := switchTable.bestPortForCoords([]byte(k))
			queue := SwitchQueue{
				ID:      k,
				Size:    v.size,
				Packets: uint64(len(v.packets)),
				Port:    uint64(nexthop),
			}
			switchqueues.Queues = append(switchqueues.Queues, queue)
		}

	}
	c.switchTable.doAdmin(getSwitchQueues)
	return switchqueues
}

// GetSessions returns a list of open sessions from this node to other nodes.
func (c *Core) GetSessions() []Session {
	var sessions []Session
	getSessions := func() {
		for _, sinfo := range c.sessions.sinfos {
			// TODO? skipped known but timed out sessions?
			session := Session{
				Coords:      append([]byte{}, sinfo.coords...),
				MTU:         sinfo.getMTU(),
				BytesSent:   sinfo.bytesSent,
				BytesRecvd:  sinfo.bytesRecvd,
				Uptime:      time.Now().Sub(sinfo.timeOpened),
				WasMTUFixed: sinfo.wasMTUFixed,
			}
			copy(session.PublicKey[:], sinfo.theirPermPub[:])
			sessions = append(sessions, session)
		}
	}
	c.router.doAdmin(getSessions)
	return sessions
}

// BuildName gets the current build name. This is usually injected if built
// from git, or returns "unknown" otherwise.
func BuildName() string {
	if buildName == "" {
		return "unknown"
	}
	return buildName
}

// BuildVersion gets the current build version. This is usually injected if
// built from git, or returns "unknown" otherwise.
func BuildVersion() string {
	if buildVersion == "" {
		return "unknown"
	}
	return buildVersion
}

// ListenConn returns a listener for Yggdrasil session connections.
func (c *Core) ConnListen() (*Listener, error) {
	c.sessions.listenerMutex.Lock()
	defer c.sessions.listenerMutex.Unlock()
	if c.sessions.listener != nil {
		return nil, errors.New("a listener already exists")
	}
	c.sessions.listener = &Listener{
		core:  c,
		conn:  make(chan *Conn),
		close: make(chan interface{}),
	}
	return c.sessions.listener, nil
}

// ConnDialer returns a dialer for Yggdrasil session connections.
func (c *Core) ConnDialer() (*Dialer, error) {
	return &Dialer{
		core: c,
	}, nil
}

// ListenTCP starts a new TCP listener. The input URI should match that of the
// "Listen" configuration item, e.g.
// 		tcp://a.b.c.d:e
func (c *Core) ListenTCP(uri string) (*TcpListener, error) {
	return c.link.tcp.listen(uri)
}

// NewEncryptionKeys generates a new encryption keypair. The encryption keys are
// used to encrypt traffic and to derive the IPv6 address/subnet of the node.
func (c *Core) NewEncryptionKeys() (*crypto.BoxPubKey, *crypto.BoxPrivKey) {
	return crypto.NewBoxKeys()
}

// NewSigningKeys generates a new signing keypair. The signing keys are used to
// derive the structure of the spanning tree.
func (c *Core) NewSigningKeys() (*crypto.SigPubKey, *crypto.SigPrivKey) {
	return crypto.NewSigKeys()
}

// NodeID gets the node ID.
func (c *Core) NodeID() *crypto.NodeID {
	return crypto.GetNodeID(&c.boxPub)
}

// TreeID gets the tree ID.
func (c *Core) TreeID() *crypto.TreeID {
	return crypto.GetTreeID(&c.sigPub)
}

// SigPubKey gets the node's signing public key.
func (c *Core) SigPubKey() string {
	return hex.EncodeToString(c.sigPub[:])
}

// BoxPubKey gets the node's encryption public key.
func (c *Core) BoxPubKey() string {
	return hex.EncodeToString(c.boxPub[:])
}

// Coords returns the current coordinates of the node.
func (c *Core) Coords() []byte {
	table := c.switchTable.table.Load().(lookupTable)
	return table.self.getCoords()
}

// Address gets the IPv6 address of the Yggdrasil node. This is always a /128
// address.
func (c *Core) Address() *net.IP {
	address := net.IP(address.AddrForNodeID(c.NodeID())[:])
	return &address
}

// Subnet gets the routed IPv6 subnet of the Yggdrasil node. This is always a
// /64 subnet.
func (c *Core) Subnet() *net.IPNet {
	subnet := address.SubnetForNodeID(c.NodeID())[:]
	subnet = append(subnet, 0, 0, 0, 0, 0, 0, 0, 0)
	return &net.IPNet{IP: subnet, Mask: net.CIDRMask(64, 128)}
}

// RouterAddresses returns the raw address and subnet types as used by the
// router
func (c *Core) RouterAddresses() (address.Address, address.Subnet) {
	return c.router.addr, c.router.subnet
}

// NodeInfo gets the currently configured nodeinfo.
func (c *Core) MyNodeInfo() nodeinfoPayload {
	return c.router.nodeinfo.getNodeInfo()
}

// SetNodeInfo the lcal nodeinfo. Note that nodeinfo can be any value or struct,
// it will be serialised into JSON automatically.
func (c *Core) SetNodeInfo(nodeinfo interface{}, nodeinfoprivacy bool) {
	c.router.nodeinfo.setNodeInfo(nodeinfo, nodeinfoprivacy)
}

// GetNodeInfo requests nodeinfo from a remote node, as specified by the public
// key and coordinates specified. The third parameter specifies whether a cached
// result is acceptable - this results in less traffic being generated than is
// necessary when, e.g. crawling the network.
func (c *Core) GetNodeInfo(keyString, coordString string, nocache bool) (NodeInfoPayload, error) {
	var key crypto.BoxPubKey
	if keyBytes, err := hex.DecodeString(keyString); err != nil {
		return NodeInfoPayload{}, err
	} else {
		copy(key[:], keyBytes)
	}
	if !nocache {
		if response, err := c.router.nodeinfo.getCachedNodeInfo(key); err == nil {
			return NodeInfoPayload(response), nil
		}
	}
	var coords []byte
	for _, cstr := range strings.Split(strings.Trim(coordString, "[]"), " ") {
		if cstr == "" {
			// Special case, happens if trimmed is the empty string, e.g. this is the root
			continue
		}
		if u64, err := strconv.ParseUint(cstr, 10, 8); err != nil {
			return NodeInfoPayload{}, err
		} else {
			coords = append(coords, uint8(u64))
		}
	}
	response := make(chan *nodeinfoPayload, 1)
	sendNodeInfoRequest := func() {
		c.router.nodeinfo.addCallback(key, func(nodeinfo *nodeinfoPayload) {
			defer func() { recover() }()
			select {
			case response <- nodeinfo:
			default:
			}
		})
		c.router.nodeinfo.sendNodeInfo(key, coords, false)
	}
	c.router.doAdmin(sendNodeInfoRequest)
	go func() {
		time.Sleep(6 * time.Second)
		close(response)
	}()
	for res := range response {
		return NodeInfoPayload(*res), nil
	}
	return NodeInfoPayload{}, errors.New(fmt.Sprintf("getNodeInfo timeout: %s", keyString))
}

// SetSessionGatekeeper allows you to configure a handler function for deciding
// whether a session should be allowed or not. The default session firewall is
// implemented in this way. The function receives the public key of the remote
// side, and a boolean which is true if we initiated the session or false if we
// received an incoming session request.
func (c *Core) SetSessionGatekeeper(f func(pubkey *crypto.BoxPubKey, initiator bool) bool) {
	c.sessions.isAllowedMutex.Lock()
	defer c.sessions.isAllowedMutex.Unlock()

	c.sessions.isAllowedHandler = f
}

// SetLogger sets the output logger of the Yggdrasil node after startup. This
// may be useful if you want to redirect the output later.
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
		return err
	}
	c.config.Mutex.Lock()
	if sintf == "" {
		c.config.Current.Peers = append(c.config.Current.Peers, addr)
	} else {
		c.config.Current.InterfacePeers[sintf] = append(c.config.Current.InterfacePeers[sintf], addr)
	}
	c.config.Mutex.Unlock()
	return nil
}

// RemovePeer is not implemented yet.
func (c *Core) RemovePeer(addr string, sintf string) error {
	// TODO: Implement a reverse of AddPeer, where we look up the port number
	// based on the addr and sintf, disconnect it and then remove it from the
	// peers list so we don't reconnect to it later
	return errors.New("not implemented")
}

// CallPeer calls a peer once. This should be specified in the peer URI format,
// e.g.:
// 		tcp://a.b.c.d:e
//		socks://a.b.c.d:e/f.g.h.i:j
// This does not add the peer to the peer list, so if the connection drops, the
// peer will not be called again automatically.
func (c *Core) CallPeer(addr string, sintf string) error {
	return c.link.call(addr, sintf)
}

// DisconnectPeer disconnects a peer once. This should be specified as a port
// number.
func (c *Core) DisconnectPeer(port uint64) error {
	c.peers.removePeer(switchPort(port))
	return nil
}

// GetAllowedEncryptionPublicKeys returns the public keys permitted for incoming
// peer connections.
func (c *Core) GetAllowedEncryptionPublicKeys() []string {
	return c.peers.getAllowedEncryptionPublicKeys()
}

// AddAllowedEncryptionPublicKey whitelists a key for incoming peer connections.
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

// Send a DHT ping to the node with the provided key and coords, optionally looking up the specified target NodeID.
func (c *Core) DHTPing(keyString, coordString, targetString string) (DHTRes, error) {
	var key crypto.BoxPubKey
	if keyBytes, err := hex.DecodeString(keyString); err != nil {
		return DHTRes{}, err
	} else {
		copy(key[:], keyBytes)
	}
	var coords []byte
	for _, cstr := range strings.Split(strings.Trim(coordString, "[]"), " ") {
		if cstr == "" {
			// Special case, happens if trimmed is the empty string, e.g. this is the root
			continue
		}
		if u64, err := strconv.ParseUint(cstr, 10, 8); err != nil {
			return DHTRes{}, err
		} else {
			coords = append(coords, uint8(u64))
		}
	}
	resCh := make(chan *dhtRes, 1)
	info := dhtInfo{
		key:    key,
		coords: coords,
	}
	target := *info.getNodeID()
	if targetString == "none" {
		// Leave the default target in place
	} else if targetBytes, err := hex.DecodeString(targetString); err != nil {
		return DHTRes{}, err
	} else if len(targetBytes) != len(target) {
		return DHTRes{}, errors.New("Incorrect target NodeID length")
	} else {
		var target crypto.NodeID
		copy(target[:], targetBytes)
	}
	rq := dhtReqKey{info.key, target}
	sendPing := func() {
		c.dht.addCallback(&rq, func(res *dhtRes) {
			defer func() { recover() }()
			select {
			case resCh <- res:
			default:
			}
		})
		c.dht.ping(&info, &target)
	}
	c.router.doAdmin(sendPing)
	go func() {
		time.Sleep(6 * time.Second)
		close(resCh)
	}()
	// TODO: do something better than the below...
	for res := range resCh {
		r := DHTRes{
			Coords: append([]byte{}, res.Coords...),
		}
		copy(r.PublicKey[:], res.Key[:])
		for _, i := range res.Infos {
			e := DHTEntry{
				Coords: append([]byte{}, i.coords...),
			}
			copy(e.PublicKey[:], i.key[:])
			r.Infos = append(r.Infos, e)
		}
		return r, nil
	}
	return DHTRes{}, errors.New(fmt.Sprintf("DHT ping timeout: %s", keyString))
}
