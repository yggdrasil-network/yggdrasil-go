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

	/*
			current := c.config.Load().(*config.NodeConfig)

			boxPrivHex, err := hex.DecodeString(current.EncryptionPrivateKey)
			if err != nil {
				return err
			}
			if len(boxPrivHex) < crypto.BoxPrivKeyLen {
				return errors.New("EncryptionPrivateKey is incorrect length")
			}

			sigPrivHex, err := hex.DecodeString(current.SigningPrivateKey)
			if err != nil {
				return err
			}
			if len(sigPrivHex) < crypto.SigPrivKeyLen {
				return errors.New("SigningPrivateKey is incorrect length")
			}

		copy(c.boxPriv[:], boxPrivHex)
		copy(c.sigPriv[:], sigPrivHex)
	*/
	/*
		copy(c.boxPriv[:], boxPrivKey[:])
		copy(c.sigPriv[:], sigPrivKey[:])

		boxPub, sigPub := c.boxPriv.Public(), c.sigPriv.Public()

		copy(c.boxPub[:], boxPub[:])
		copy(c.sigPub[:], sigPub[:])
	*/
	/*
		if bp := hex.EncodeToString(c.boxPub[:]); current.EncryptionPublicKey != bp {
			c.log.Warnln("EncryptionPublicKey in config is incorrect, should be", bp)
		}
		if sp := hex.EncodeToString(c.sigPub[:]); current.SigningPublicKey != sp {
			c.log.Warnln("SigningPublicKey in config is incorrect, should be", sp)
		}
	*/

	c.peers.init(c)
	c.router.init(c, *boxPrivKey)
	c.switchTable.init(c, *sigPrivKey) // TODO move before peers? before router?

	return nil
}

// If any static peers were provided in the configuration above then we should
// configure them. The loop ensures that disconnected peers will eventually
// be reconnected with.
func (c *Core) _addPeerLoop() {
	// TODO: PERSISTENCE
	/*
		// Get the peers from the config - these could change!
		current := c.GetConfig()

		// Add peers from the Peers section
		for _, peer := range current.Peers {
			go c.AddPeer(peer, "") // TODO: this should be acted and not in a goroutine?
			time.Sleep(time.Second)
		}

		// Add peers from the InterfacePeers section
		for intf, intfpeers := range current.InterfacePeers {
			for _, peer := range intfpeers {
				go c.AddPeer(peer, intf) // TODO: this should be acted and not in a goroutine?
				time.Sleep(time.Second)
			}
		}

		// Sit for a while
		time.AfterFunc(time.Minute, func() {
			c.Act(c, c._addPeerLoop)
		})
	*/
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

	c.Act(c, c._addPeerLoop)

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
