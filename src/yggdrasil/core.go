package yggdrasil

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"regexp"
	"sync"

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
	config      config.NodeConfig // Active config
	configOld   config.NodeConfig // Previous config
	configMutex sync.RWMutex      // Protects both config and configOld
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
	nodeinfo    nodeinfo
	tcp         tcpInterface
	log         *log.Logger
	ifceExpr    []*regexp.Regexp // the zone of link-local IPv6 peers must match this
}

func (c *Core) init() error {
	// TODO separate init and start functions
	//  Init sets up structs
	//  Start launches goroutines that depend on structs being set up
	// This is pretty much required to completely avoid race conditions
	if c.log == nil {
		c.log = log.New(ioutil.Discard, "", 0)
	}

	boxPubHex, err := hex.DecodeString(c.config.EncryptionPublicKey)
	if err != nil {
		return err
	}
	boxPrivHex, err := hex.DecodeString(c.config.EncryptionPrivateKey)
	if err != nil {
		return err
	}
	sigPubHex, err := hex.DecodeString(c.config.SigningPublicKey)
	if err != nil {
		return err
	}
	sigPrivHex, err := hex.DecodeString(c.config.SigningPrivateKey)
	if err != nil {
		return err
	}

	copy(c.boxPub[:], boxPubHex)
	copy(c.boxPriv[:], boxPrivHex)
	copy(c.sigPub[:], sigPubHex)
	copy(c.sigPriv[:], sigPrivHex)

	c.admin.init(c)
	c.nodeinfo.init(c)
	c.searches.init(c)
	c.dht.init(c)
	c.sessions.init(c)
	c.multicast.init(c)
	c.peers.init(c)
	c.router.init(c)
	c.switchTable.init(c) // TODO move before peers? before router?

	if err := c.tcp.init(c); err != nil {
		c.log.Println("Failed to start TCP interface")
		return err
	}

	return nil
}

// UpdateConfig updates the configuration in Core and then signals the
// various module goroutines to reconfigure themselves if needed
func (c *Core) UpdateConfig(config *config.NodeConfig) {
	c.configMutex.Lock()
	c.configOld = c.config
	c.config = *config
	c.configMutex.Unlock()

	c.admin.reconfigure <- true
	c.searches.reconfigure <- true
	c.dht.reconfigure <- true
	c.sessions.reconfigure <- true
	c.multicast.reconfigure <- true
	c.peers.reconfigure <- true
	c.router.reconfigure <- true
	c.switchTable.reconfigure <- true
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
		c.log.Println("Build name:", name)
	}
	if version := GetBuildVersion(); version != "unknown" {
		c.log.Println("Build version:", version)
	}

	c.log.Println("Starting up...")

	c.configMutex.Lock()
	c.config = *nc
	c.configOld = c.config
	c.configMutex.Unlock()

	c.init()

	c.nodeinfo.setNodeInfo(nc.NodeInfo, nc.NodeInfoPrivacy)

	if nc.SwitchOptions.MaxTotalQueueSize >= SwitchQueueTotalMinSize {
		c.switchTable.queueTotalMaxSize = nc.SwitchOptions.MaxTotalQueueSize
	}

	if err := c.switchTable.start(); err != nil {
		c.log.Println("Failed to start switch")
		return err
	}

	c.sessions.setSessionFirewallState(nc.SessionFirewall.Enable)
	c.sessions.setSessionFirewallDefaults(
		nc.SessionFirewall.AllowFromDirect,
		nc.SessionFirewall.AllowFromRemote,
		nc.SessionFirewall.AlwaysAllowOutbound,
	)
	c.sessions.setSessionFirewallWhitelist(nc.SessionFirewall.WhitelistEncryptionPublicKeys)
	c.sessions.setSessionFirewallBlacklist(nc.SessionFirewall.BlacklistEncryptionPublicKeys)

	if err := c.router.start(); err != nil {
		c.log.Println("Failed to start router")
		return err
	}

	c.router.cryptokey.setEnabled(nc.TunnelRouting.Enable)
	if c.router.cryptokey.isEnabled() {
		c.log.Println("Crypto-key routing enabled")
		for ipv6, pubkey := range nc.TunnelRouting.IPv6Destinations {
			if err := c.router.cryptokey.addRoute(ipv6, pubkey); err != nil {
				panic(err)
			}
		}
		for _, source := range nc.TunnelRouting.IPv6Sources {
			if err := c.router.cryptokey.addSourceSubnet(source); err != nil {
				panic(err)
			}
		}
		for ipv4, pubkey := range nc.TunnelRouting.IPv4Destinations {
			if err := c.router.cryptokey.addRoute(ipv4, pubkey); err != nil {
				panic(err)
			}
		}
		for _, source := range nc.TunnelRouting.IPv4Sources {
			if err := c.router.cryptokey.addSourceSubnet(source); err != nil {
				panic(err)
			}
		}
	}

	if err := c.admin.start(); err != nil {
		c.log.Println("Failed to start admin socket")
		return err
	}

	if err := c.multicast.start(); err != nil {
		c.log.Println("Failed to start multicast interface")
		return err
	}

	ip := net.IP(c.router.addr[:]).String()
	if err := c.router.tun.start(nc.IfName, nc.IfTAPMode, fmt.Sprintf("%s/%d", ip, 8*len(address.GetPrefix())-1), nc.IfMTU); err != nil {
		c.log.Println("Failed to start TUN/TAP")
		return err
	}

	c.log.Println("Startup complete")
	return nil
}

// Stops the Yggdrasil node.
func (c *Core) Stop() {
	c.log.Println("Stopping...")
	c.router.tun.close()
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
	return c.nodeinfo.getNodeInfo()
}

// Sets the nodeinfo.
func (c *Core) SetNodeInfo(nodeinfo interface{}, nodeinfoprivacy bool) {
	c.nodeinfo.setNodeInfo(nodeinfo, nodeinfoprivacy)
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

// Adds an expression to select multicast interfaces for peer discovery. This
// should be done before calling Start. This function can be called multiple
// times to add multiple search expressions.
func (c *Core) AddMulticastInterfaceExpr(expr *regexp.Regexp) {
	c.ifceExpr = append(c.ifceExpr, expr)
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
	return c.router.tun.iface.Name()
}

// Gets the current TUN/TAP interface MTU.
func (c *Core) GetTUNIfMTU() int {
	return c.router.tun.mtu
}
