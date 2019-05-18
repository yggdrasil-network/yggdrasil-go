package yggdrasil

import (
	"encoding/hex"
	"io/ioutil"
	"time"

	"github.com/gologme/log"

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
	c.log.Debugln("Reloading node configuration...")

	c.config.Replace(*config)

	errors := 0

	components := []chan chan error{
		c.admin.reconfigure,
		c.searches.reconfigure,
		c.dht.reconfigure,
		c.sessions.reconfigure,
		c.peers.reconfigure,
		c.router.reconfigure,
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
		c.log.Warnln(errors, "node module(s) reported errors during configuration reload")
	} else {
		c.log.Infoln("Node configuration reloaded successfully")
	}
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

	go c.addPeerLoop()

	c.log.Infoln("Startup complete")
	return &c.config, nil
}

// Stop shuts down the Yggdrasil node.
func (c *Core) Stop() {
	c.log.Infoln("Stopping...")
	c.admin.close()
}
