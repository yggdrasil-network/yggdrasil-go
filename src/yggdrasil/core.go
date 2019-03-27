package yggdrasil

import (
	"encoding/hex"
	"io/ioutil"
	"net"
	"time"

	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
)

var buildName string
var buildVersion string

type module interface {
	init(*Core, *config.NodeConfig) error
	start() error
}

// The Core object represents the Yggdrasil node. You should create a Core
// object for each Yggdrasil node you plan to run.
type Core struct {
	// This is the main data structure that holds everything else for a node
	// We're going to keep our own copy of the provided config - that way we can
	// guarantee that it will be covered by the mutex
	config      config.StatefulNodeConfig
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
	multicast   multicast
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

	boxPubHex, err := hex.DecodeString(c.config.Current.EncryptionPublicKey)
	if err != nil {
		return err
	}
	boxPrivHex, err := hex.DecodeString(c.config.Current.EncryptionPrivateKey)
	if err != nil {
		return err
	}
	sigPubHex, err := hex.DecodeString(c.config.Current.SigningPublicKey)
	if err != nil {
		return err
	}
	sigPrivHex, err := hex.DecodeString(c.config.Current.SigningPrivateKey)
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
	c.multicast.init(c)
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
		// Get the peers from the config - these could change!
		c.config.Mutex.RLock()
		peers := c.config.Current.Peers
		interfacepeers := c.config.Current.InterfacePeers
		c.config.Mutex.RUnlock()

		// Add peers from the Peers section
		for _, peer := range peers {
			c.AddPeer(peer, "")
			time.Sleep(time.Second)
		}

		// Add peers from the InterfacePeers section
		for intf, intfpeers := range interfacepeers {
			for _, peer := range intfpeers {
				c.AddPeer(peer, intf)
				time.Sleep(time.Second)
			}
		}

		// Sit for a while
		time.Sleep(time.Minute)
	}
}

// UpdateConfig updates the configuration in Core and then signals the
// various module goroutines to reconfigure themselves if needed
func (c *Core) UpdateConfig(config *config.NodeConfig) {
	c.log.Infoln("Reloading configuration...")

	c.config.Mutex.Lock()
	c.config.Previous = c.config.Current
	c.config.Current = *config
	c.config.Mutex.Unlock()

	errors := 0

	components := []chan chan error{
		c.admin.reconfigure,
		c.searches.reconfigure,
		c.dht.reconfigure,
		c.sessions.reconfigure,
		c.peers.reconfigure,
		c.router.reconfigure,
		c.router.tun.Reconfigure,
		c.router.cryptokey.reconfigure,
		c.switchTable.reconfigure,
		c.link.reconfigure,
		c.multicast.reconfigure,
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

// GetBuildName gets the current build name. This is usually injected if built
// from git, or returns "unknown" otherwise.
func GetBuildName() string {
	if buildName == "" {
		return "unknown"
	}
	return buildName
}

// Get the current build version. This is usually injected if built from git,
// or returns "unknown" otherwise.
func GetBuildVersion() string {
	if buildVersion == "" {
		return "unknown"
	}
	return buildVersion
}

// Starts up Yggdrasil using the provided NodeConfig, and outputs debug logging
// through the provided log.Logger. The started stack will include TCP and UDP
// sockets, a multicast discovery socket, an admin socket, router, switch and
// DHT node.
func (c *Core) Start(nc *config.NodeConfig, log *log.Logger) error {
	c.log = log

	if name := GetBuildName(); name != "unknown" {
		c.log.Infoln("Build name:", name)
	}
	if version := GetBuildVersion(); version != "unknown" {
		c.log.Infoln("Build version:", version)
	}

	c.log.Infoln("Starting up...")

	c.config.Mutex.Lock()
	c.config.Current = *nc
	c.config.Previous = *nc
	c.config.Mutex.Unlock()

	c.init()

	if err := c.link.init(c); err != nil {
		c.log.Errorln("Failed to start link interfaces")
		return err
	}

	if nc.SwitchOptions.MaxTotalQueueSize >= SwitchQueueTotalMinSize {
		c.switchTable.queueTotalMaxSize = nc.SwitchOptions.MaxTotalQueueSize
	}

	if err := c.switchTable.start(); err != nil {
		c.log.Errorln("Failed to start switch")
		return err
	}

	if err := c.router.start(); err != nil {
		c.log.Errorln("Failed to start router")
		return err
	}

	if err := c.admin.start(); err != nil {
		c.log.Errorln("Failed to start admin socket")
		return err
	}

	if err := c.multicast.start(); err != nil {
		c.log.Errorln("Failed to start multicast interface")
		return err
	}

	if err := c.router.tun.Start(c.router.addr, c.router.subnet); err != nil {
		c.log.Errorln("Failed to start TUN/TAP")
		return err
	}

	go c.addPeerLoop()

	c.log.Infoln("Startup complete")
	return nil
}

// Stops the Yggdrasil node.
func (c *Core) Stop() {
	c.log.Infoln("Stopping...")
	c.router.tun.Close()
	c.admin.close()
}

// Generates a new encryption keypair. The encryption keys are used to
// encrypt traffic and to derive the IPv6 address/subnet of the node.
func (c *Core) NewEncryptionKeys() (*crypto.BoxPubKey, *crypto.BoxPrivKey) {
	return crypto.NewBoxKeys()
}

// Generates a new signing keypair. The signing keys are used to derive the
// structure of the spanning tree.
func (c *Core) NewSigningKeys() (*crypto.SigPubKey, *crypto.SigPrivKey) {
	return crypto.NewSigKeys()
}

// Gets the node ID.
func (c *Core) GetNodeID() *crypto.NodeID {
	return crypto.GetNodeID(&c.boxPub)
}

// Gets the tree ID.
func (c *Core) GetTreeID() *crypto.TreeID {
	return crypto.GetTreeID(&c.sigPub)
}

// Gets the IPv6 address of the Yggdrasil node. This is always a /128.
func (c *Core) GetAddress() *net.IP {
	address := net.IP(address.AddrForNodeID(c.GetNodeID())[:])
	return &address
}

// Gets the routed IPv6 subnet of the Yggdrasil node. This is always a /64.
func (c *Core) GetSubnet() *net.IPNet {
	subnet := address.SubnetForNodeID(c.GetNodeID())[:]
	subnet = append(subnet, 0, 0, 0, 0, 0, 0, 0, 0)
	return &net.IPNet{IP: subnet, Mask: net.CIDRMask(64, 128)}
}

// Gets the nodeinfo.
func (c *Core) GetNodeInfo() nodeinfoPayload {
	return c.router.nodeinfo.getNodeInfo()
}

// Sets the nodeinfo.
func (c *Core) SetNodeInfo(nodeinfo interface{}, nodeinfoprivacy bool) {
	c.router.nodeinfo.setNodeInfo(nodeinfo, nodeinfoprivacy)
}

// Sets the output logger of the Yggdrasil node after startup. This may be
// useful if you want to redirect the output later.
func (c *Core) SetLogger(log *log.Logger) {
	c.log = log
}

// Adds a peer. This should be specified in the peer URI format, i.e.
// tcp://a.b.c.d:e, udp://a.b.c.d:e, socks://a.b.c.d:e/f.g.h.i:j
func (c *Core) AddPeer(addr string, sintf string) error {
	return c.admin.addPeer(addr, sintf)
}

// Adds an allowed public key. This allow peerings to be restricted only to
// keys that you have selected.
func (c *Core) AddAllowedEncryptionPublicKey(boxStr string) error {
	return c.admin.addAllowedEncryptionPublicKey(boxStr)
}

// Gets the default admin listen address for your platform.
func (c *Core) GetAdminDefaultListen() string {
	return defaults.GetDefaults().DefaultAdminListen
}

// Gets the default TUN/TAP interface name for your platform.
func (c *Core) GetTUNDefaultIfName() string {
	return defaults.GetDefaults().DefaultIfName
}

// Gets the default TUN/TAP interface MTU for your platform. This can be as high
// as 65535, depending on platform, but is never lower than 1280.
func (c *Core) GetTUNDefaultIfMTU() int {
	return defaults.GetDefaults().DefaultIfMTU
}

// Gets the maximum supported TUN/TAP interface MTU for your platform. This
// can be as high as 65535, depending on platform, but is never lower than 1280.
func (c *Core) GetTUNMaximumIfMTU() int {
	return defaults.GetDefaults().MaximumIfMTU
}

// Gets the default TUN/TAP interface mode for your platform.
func (c *Core) GetTUNDefaultIfTAPMode() bool {
	return defaults.GetDefaults().DefaultIfTAPMode
}

// Gets the current TUN/TAP interface name.
func (c *Core) GetTUNIfName() string {
	return c.router.tun.GetName()
}

// Gets the current TUN/TAP interface MTU.
func (c *Core) GetTUNIfMTU() int {
	return c.router.tun.GetMTU()
}
