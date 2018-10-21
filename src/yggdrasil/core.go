package yggdrasil

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"regexp"

	"yggdrasil/config"
	"yggdrasil/defaults"
)

// The Core object represents the Yggdrasil node. You should create a Core
// object for each Yggdrasil node you plan to run.
type Core struct {
	// This is the main data structure that holds everything else for a node
	boxPub       boxPubKey
	boxPriv      boxPrivKey
	sigPub       sigPubKey
	sigPriv      sigPrivKey
	friendlyName string
	switchTable  switchTable
	peers        peers
	sigs         sigManager
	sessions     sessions
	router       router
	dht          dht
	tun          tunDevice
	admin        admin
	searches     searches
	multicast    multicast
	tcp          tcpInterface
	log          *log.Logger
	ifceExpr     []*regexp.Regexp // the zone of link-local IPv6 peers must match this
}

func (c *Core) init(bpub *boxPubKey,
	bpriv *boxPrivKey,
	spub *sigPubKey,
	spriv *sigPrivKey,
	friendlyname string) {
	// TODO separate init and start functions
	//  Init sets up structs
	//  Start launches goroutines that depend on structs being set up
	// This is pretty much required to completely avoid race conditions
	util_initByteStore()
	if c.log == nil {
		c.log = log.New(ioutil.Discard, "", 0)
	}
	c.boxPub, c.boxPriv = *bpub, *bpriv
	c.sigPub, c.sigPriv = *spub, *spriv
	c.friendlyName = friendlyname
	c.admin.core = c
	c.sigs.init()
	c.searches.init(c)
	c.dht.init(c)
	c.sessions.init(c)
	c.multicast.init(c)
	c.peers.init(c)
	c.router.init(c)
	c.switchTable.init(c, c.sigPub) // TODO move before peers? before router?
	c.tun.init(c)
}

// Gets the friendly name of this node, as specified in the NodeConfig.
func (c *Core) GetFriendlyName() string {
	if c.friendlyName == "" {
		return "(none)"
	}
	return c.friendlyName
}

// Starts up Yggdrasil using the provided NodeConfig, and outputs debug logging
// through the provided log.Logger. The started stack will include TCP and UDP
// sockets, a multicast discovery socket, an admin socket, router, switch and
// DHT node.
func (c *Core) Start(nc *config.NodeConfig, log *log.Logger) error {
	c.log = log
	c.log.Println("Starting up...")

	var boxPub boxPubKey
	var boxPriv boxPrivKey
	var sigPub sigPubKey
	var sigPriv sigPrivKey
	boxPubHex, err := hex.DecodeString(nc.EncryptionPublicKey)
	if err != nil {
		return err
	}
	boxPrivHex, err := hex.DecodeString(nc.EncryptionPrivateKey)
	if err != nil {
		return err
	}
	sigPubHex, err := hex.DecodeString(nc.SigningPublicKey)
	if err != nil {
		return err
	}
	sigPrivHex, err := hex.DecodeString(nc.SigningPrivateKey)
	if err != nil {
		return err
	}
	copy(boxPub[:], boxPubHex)
	copy(boxPriv[:], boxPrivHex)
	copy(sigPub[:], sigPubHex)
	copy(sigPriv[:], sigPrivHex)

	c.init(&boxPub, &boxPriv, &sigPub, &sigPriv, nc.FriendlyName)
	c.admin.init(c, nc.AdminListen)

	if err := c.tcp.init(c, nc.Listen, nc.ReadTimeout); err != nil {
		c.log.Println("Failed to start TCP interface")
		return err
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

	if err := c.admin.start(); err != nil {
		c.log.Println("Failed to start admin socket")
		return err
	}

	if err := c.multicast.start(); err != nil {
		c.log.Println("Failed to start multicast interface")
		return err
	}

	ip := net.IP(c.router.addr[:]).String()
	if err := c.tun.start(nc.IfName, nc.IfTAPMode, fmt.Sprintf("%s/%d", ip, 8*len(address_prefix)-1), nc.IfMTU); err != nil {
		c.log.Println("Failed to start TUN/TAP")
		return err
	}

	c.log.Println("Startup complete")
	return nil
}

// Stops the Yggdrasil node.
func (c *Core) Stop() {
	c.log.Println("Stopping...")
	c.tun.close()
	c.admin.close()
}

// Generates a new encryption keypair. The encryption keys are used to
// encrypt traffic and to derive the IPv6 address/subnet of the node.
func (c *Core) NewEncryptionKeys() (*boxPubKey, *boxPrivKey) {
	return newBoxKeys()
}

// Generates a new signing keypair. The signing keys are used to derive the
// structure of the spanning tree.
func (c *Core) NewSigningKeys() (*sigPubKey, *sigPrivKey) {
	return newSigKeys()
}

// Gets the node ID.
func (c *Core) GetNodeID() *NodeID {
	return getNodeID(&c.boxPub)
}

// Gets the tree ID.
func (c *Core) GetTreeID() *TreeID {
	return getTreeID(&c.sigPub)
}

// Gets the IPv6 address of the Yggdrasil node. This is always a /128.
func (c *Core) GetAddress() *net.IP {
	address := net.IP(address_addrForNodeID(c.GetNodeID())[:])
	return &address
}

// Gets the routed IPv6 subnet of the Yggdrasil node. This is always a /64.
func (c *Core) GetSubnet() *net.IPNet {
	subnet := address_subnetForNodeID(c.GetNodeID())[:]
	subnet = append(subnet, 0, 0, 0, 0, 0, 0, 0, 0)
	return &net.IPNet{IP: subnet, Mask: net.CIDRMask(64, 128)}
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
	return c.tun.iface.Name()
}

// Gets the current TUN/TAP interface MTU.
func (c *Core) GetTUNIfMTU() int {
	return c.tun.mtu
}
