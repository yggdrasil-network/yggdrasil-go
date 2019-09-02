package yggdrasil

import (
	"io/ioutil"

	"github.com/Arceliar/phony"
	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"
)

// The Core object represents the Yggdrasil node. You should create a Core
// object for each Yggdrasil node you plan to run.
type Core struct {
	// This is the main data structure that holds everything else for a node
	// We're going to keep our own copy of the provided config - that way we can
	// guarantee that it will be covered by the mutex
	phony.Inbox
	switchTable switchTable
	peers       peers
	router      router
	link        link
	log         *log.Logger
}

func (c *Core) _init(boxPrivKey *crypto.BoxPrivKey, sigPrivKey *crypto.SigPrivKey) error {
	// TODO separate init and start functions
	//  Init sets up structs
	//  Start launches goroutines that depend on structs being set up
	// This is pretty much required to completely avoid race conditions
	if c.log == nil {
		c.log = log.New(ioutil.Discard, "", 0)
	}

	c.peers.init(c)
	c.router.init(c, *boxPrivKey)
	c.switchTable.init(c, *sigPrivKey) // TODO move before peers? before router?

	return nil
}

// Start starts up Yggdrasil using the provided config.NodeConfig, and outputs
// debug logging through the provided log.Logger. The started stack will include
// TCP and UDP sockets, a multicast discovery socket, an admin socket, router,
// switch and DHT node. A config.NodeState is returned which contains both the
// current and previous configurations (from reconfigures).
func (c *Core) Start(boxPrivKey *crypto.BoxPrivKey, sigPrivKey *crypto.SigPrivKey, log *log.Logger) (err error) {
	phony.Block(c, func() {
		err = c._start(boxPrivKey, sigPrivKey, log)
	})
	return
}

// This function is unsafe and should only be ran by the core actor.
func (c *Core) _start(boxPrivKey *crypto.BoxPrivKey, sigPrivKey *crypto.SigPrivKey, log *log.Logger) error {
	c.log = log
	//c.config.Store(nc)

	if name := version.BuildName(); name != "unknown" {
		c.log.Infoln("Build name:", name)
	}
	if version := version.BuildVersion(); version != "unknown" {
		c.log.Infoln("Build version:", version)
	}

	c.log.Infoln("Starting up...")
	c._init(boxPrivKey, sigPrivKey)

	if err := c.link.init(c); err != nil {
		c.log.Errorln("Failed to start link interfaces")
		return err
	}

	if err := c.switchTable.start(); err != nil {
		c.log.Errorln("Failed to start switch")
		return err
	}

	if err := c.router.start(); err != nil {
		c.log.Errorln("Failed to start router")
		return err
	}

	c.peers.Act(c, c.peers._addPeerLoop)

	c.log.Infoln("Startup complete")
	return nil
}

// Stop shuts down the Yggdrasil node.
func (c *Core) Stop() {
	phony.Block(c, c._stop)
}

// This function is unsafe and should only be ran by the core actor.
func (c *Core) _stop() {
	c.log.Infoln("Stopping...")
}
