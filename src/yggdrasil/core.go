package yggdrasil

import (
	"encoding/hex"
	"errors"
	"io/ioutil"
	"net"
	"sync"
	"time"

	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

var buildName string
var buildVersion string

// The Core object represents the Yggdrasil node. You should create a Core
// object for each Yggdrasil node you plan to run.
type Core struct {
	// This is the main data structure that holds everything else for a node
	// We're going to keep our own copy of the provided config - that way we can
	// guarantee that it will be covered by the mutex
	config      config.NodeState // Config
	boxPub      crypto.BoxPubKey
	boxPriv     crypto.BoxPrivKey
	sigPub      crypto.SigPubKey
	sigPriv     crypto.SigPrivKey
	switchTable switchTable
	peers       peers
	sessions    sessions
	router      router
	dht         dht
	admin       admin
	searches    searches
	link        link
	log         *log.Logger
}

func (c *Core) init() error {
	// TODO separate init and start functions
	//  Init sets up structs
	//  Start launches goroutines that depend on structs being set up
	// This is pretty much required to completely avoid race conditions
	if c.log == nil {
		c.log = log.New(ioutil.Discard, "", 0)
	}

	current, _ := c.config.Get()

	boxPubHex, err := hex.DecodeString(current.EncryptionPublicKey)
	if err != nil {
		return err
	}
	boxPrivHex, err := hex.DecodeString(current.EncryptionPrivateKey)
	if err != nil {
		return err
	}
	sigPubHex, err := hex.DecodeString(current.SigningPublicKey)
	if err != nil {
		return err
	}
	sigPrivHex, err := hex.DecodeString(current.SigningPrivateKey)
	if err != nil {
		return err
	}

	copy(c.boxPub[:], boxPubHex)
	copy(c.boxPriv[:], boxPrivHex)
	copy(c.sigPub[:], sigPubHex)
	copy(c.sigPriv[:], sigPrivHex)

	c.admin.init(c)
	c.searches.init(c)
	c.dht.init(c)
	c.sessions.init(c)
	//c.multicast.init(c)
	c.peers.init(c)
	c.router.init(c)
	c.switchTable.init(c) // TODO move before peers? before router?

	return nil
}

// If any static peers were provided in the configuration above then we should
// configure them. The loop ensures that disconnected peers will eventually
// be reconnected with.
func (c *Core) addPeerLoop() {
	for {
		//  the peers from the config - these could change!
		current, _ := c.config.Get()

		// Add peers from the Peers section
		for _, peer := range current.Peers {
			c.AddPeer(peer, "")
			time.Sleep(time.Second)
		}

		// Add peers from the InterfacePeers section
		for intf, intfpeers := range current.InterfacePeers {
			for _, peer := range intfpeers {
				c.AddPeer(peer, intf)
				time.Sleep(time.Second)
			}
		}

		// Sit for a while
		time.Sleep(time.Minute)
	}
}

// UpdateConfig updates the configuration in Core with the provided
// config.NodeConfig and then signals the various module goroutines to
// reconfigure themselves if needed.
func (c *Core) UpdateConfig(config *config.NodeConfig) {
	c.log.Infoln("Reloading configuration...")

	c.config.Replace(*config)

	errors := 0

	components := []chan chan error{
		c.admin.reconfigure,
		c.searches.reconfigure,
		c.dht.reconfigure,
		c.sessions.reconfigure,
		c.peers.reconfigure,
		c.router.reconfigure,
		c.router.cryptokey.reconfigure,
		c.switchTable.reconfigure,
		c.link.reconfigure,
	}

	for _, component := range components {
		response := make(chan error)
		component <- response
		if err := <-response; err != nil {
			c.log.Errorln(err)
			errors++
		}
	}

	if errors > 0 {
		c.log.Warnln(errors, "modules reported errors during configuration reload")
	} else {
		c.log.Infoln("Configuration reloaded successfully")
	}
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

// SetRouterAdapter instructs Yggdrasil to use the given adapter when starting
// the router. The adapter must implement the standard
// adapter.adapterImplementation interface and should extend the adapter.Adapter
// struct.
func (c *Core) SetRouterAdapter(adapter interface{}) error {
	// We do this because adapterImplementation is not a valid type for the
	// gomobile bindings so we just ask for a generic interface and try to cast it
	// to adapterImplementation instead
	if a, ok := adapter.(adapterImplementation); ok {
		c.router.adapter = a
		return nil
	}
	return errors.New("unsuitable adapter")
}

// Start starts up Yggdrasil using the provided config.NodeConfig, and outputs
// debug logging through the provided log.Logger. The started stack will include
// TCP and UDP sockets, a multicast discovery socket, an admin socket, router,
// switch and DHT node. A config.NodeState is returned which contains both the
// current and previous configurations (from reconfigures).
func (c *Core) Start(nc *config.NodeConfig, log *log.Logger) (*config.NodeState, error) {
	c.log = log

	c.config = config.NodeState{
		Current:  *nc,
		Previous: *nc,
	}

	if name := BuildName(); name != "unknown" {
		c.log.Infoln("Build name:", name)
	}
	if version := BuildVersion(); version != "unknown" {
		c.log.Infoln("Build version:", version)
	}

	c.log.Infoln("Starting up...")

	c.init()

	if err := c.link.init(c); err != nil {
		c.log.Errorln("Failed to start link interfaces")
		return nil, err
	}

	c.config.Mutex.RLock()
	if c.config.Current.SwitchOptions.MaxTotalQueueSize >= SwitchQueueTotalMinSize {
		c.switchTable.queueTotalMaxSize = c.config.Current.SwitchOptions.MaxTotalQueueSize
	}
	c.config.Mutex.RUnlock()

	if err := c.switchTable.start(); err != nil {
		c.log.Errorln("Failed to start switch")
		return nil, err
	}

	if err := c.router.start(); err != nil {
		c.log.Errorln("Failed to start router")
		return nil, err
	}

	if err := c.admin.start(); err != nil {
		c.log.Errorln("Failed to start admin socket")
		return nil, err
	}

	if c.router.adapter != nil {
		if err := c.router.adapter.Start(c.router.addr, c.router.subnet); err != nil {
			c.log.Errorln("Failed to start TUN/TAP")
			return nil, err
		}
	}

	go c.addPeerLoop()

	c.log.Infoln("Startup complete")
	return &c.config, nil
}

// Stop shuts down the Yggdrasil node.
func (c *Core) Stop() {
	c.log.Infoln("Stopping...")
	if c.router.adapter != nil {
		c.router.adapter.Close()
	}
	c.admin.close()
}

// ListenConn returns a listener for Yggdrasil session connections.
func (c *Core) ListenConn() (*Listener, error) {
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

// Dial opens a session to the given node. The first paramter should be "nodeid"
// and the second parameter should contain a hexadecimal representation of the
// target node ID.
func (c *Core) Dial(network, address string) (Conn, error) {
	conn := Conn{
		sessionMutex: &sync.RWMutex{},
	}
	nodeID := crypto.NodeID{}
	nodeMask := crypto.NodeID{}
	// Process
	switch network {
	case "nodeid":
		// A node ID was provided - we don't need to do anything special with it
		dest, err := hex.DecodeString(address)
		if err != nil {
			return Conn{}, err
		}
		copy(nodeID[:], dest)
		for i := range nodeMask {
			nodeMask[i] = 0xFF
		}
	default:
		// An unexpected address type was given, so give up
		return Conn{}, errors.New("unexpected address type")
	}
	conn.core = c
	conn.nodeID = &nodeID
	conn.nodeMask = &nodeMask
	conn.core.router.doAdmin(func() {
		conn.startSearch()
	})
	conn.sessionMutex.Lock()
	defer conn.sessionMutex.Unlock()
	return conn, nil
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
func (c *Core) NodeInfo() nodeinfoPayload {
	return c.router.nodeinfo.getNodeInfo()
}

// SetNodeInfo the lcal nodeinfo. Note that nodeinfo can be any value or struct,
// it will be serialised into JSON automatically.
func (c *Core) SetNodeInfo(nodeinfo interface{}, nodeinfoprivacy bool) {
	c.router.nodeinfo.setNodeInfo(nodeinfo, nodeinfoprivacy)
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

// CallPeer calls a peer once. This should be specified in the peer URI format,
// e.g.:
// 		tcp://a.b.c.d:e
//		socks://a.b.c.d:e/f.g.h.i:j
// This does not add the peer to the peer list, so if the connection drops, the
// peer will not be called again automatically.
func (c *Core) CallPeer(addr string, sintf string) error {
	return c.link.call(addr, sintf)
}

// AddAllowedEncryptionPublicKey adds an allowed public key. This allow peerings
// to be restricted only to keys that you have selected.
func (c *Core) AddAllowedEncryptionPublicKey(boxStr string) error {
	return c.admin.addAllowedEncryptionPublicKey(boxStr)
}
