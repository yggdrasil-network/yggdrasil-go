// +build mobile

package yggdrasil

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"time"

	"github.com/gologme/log"

	hjson "github.com/hjson/hjson-go"
	"github.com/mitchellh/mapstructure"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

// This file is meant to "plug the gap" for mobile support, as Gomobile will
// not create headers for Swift/Obj-C etc if they have complex (non-native)
// types. Therefore for iOS we will expose some nice simple functions. Note
// that in the case of iOS we handle reading/writing to/from TUN in Swift
// therefore we use the "dummy" TUN interface instead.

func (c *Core) addStaticPeers(cfg *config.NodeConfig) {
	if len(cfg.Peers) == 0 && len(cfg.InterfacePeers) == 0 {
		return
	}
	for {
		for _, peer := range cfg.Peers {
			c.AddPeer(peer, "")
			time.Sleep(time.Second)
		}
		for intf, intfpeers := range cfg.InterfacePeers {
			for _, peer := range intfpeers {
				c.AddPeer(peer, intf)
				time.Sleep(time.Second)
			}
		}
		time.Sleep(time.Minute)
	}
}

// Starts a node with a randomly generated config.
func (c *Core) StartAutoconfigure() error {
	mobilelog := MobileLogger{}
	logger := log.New(mobilelog, "", 0)
	nc := config.GenerateConfig()
	nc.IfName = "dummy"
	nc.AdminListen = "tcp://localhost:9001"
	nc.Peers = []string{}
	if hostname, err := os.Hostname(); err == nil {
		nc.NodeInfo = map[string]interface{}{"name": hostname}
	}
	if err := c.Start(nc, logger); err != nil {
		return err
	}
	go c.addStaticPeers(nc)
	return nil
}

// Starts a node with the given JSON config. You can get JSON config (rather
// than HJSON) by using the GenerateConfigJSON() function.
func (c *Core) StartJSON(configjson []byte) error {
	mobilelog := MobileLogger{}
	logger := log.New(mobilelog, "", 0)
	nc := config.GenerateConfig()
	var dat map[string]interface{}
	if err := hjson.Unmarshal(configjson, &dat); err != nil {
		return err
	}
	if err := mapstructure.Decode(dat, &nc); err != nil {
		return err
	}
	nc.IfName = "dummy"
	if err := c.Start(nc, logger); err != nil {
		return err
	}
	go c.addStaticPeers(nc)
	return nil
}

// Generates mobile-friendly configuration in JSON format.
func GenerateConfigJSON() []byte {
	nc := config.GenerateConfig()
	nc.IfName = "dummy"
	if json, err := json.Marshal(nc); err == nil {
		return json
	} else {
		return nil
	}
}

// Gets the node's IPv6 address.
func (c *Core) GetAddressString() string {
	return c.GetAddress().String()
}

// Gets the node's IPv6 subnet in CIDR notation.
func (c *Core) GetSubnetString() string {
	return c.GetSubnet().String()
}

// Gets the node's public encryption key.
func (c *Core) GetBoxPubKeyString() string {
	return hex.EncodeToString(c.boxPub[:])
}

// Gets the node's public signing key.
func (c *Core) GetSigPubKeyString() string {
	return hex.EncodeToString(c.sigPub[:])
}

// Wait for a packet from the router. You will use this when implementing a
// dummy adapter in place of real TUN - when this call returns a packet, you
// will probably want to give it to the OS to write to TUN.
func (c *Core) RouterRecvPacket() ([]byte, error) {
	packet := <-c.router.tun.Recv
	return packet, nil
}

// Send a packet to the router. You will use this when implementing a
// dummy adapter in place of real TUN - when the operating system tells you
// that a new packet is available from TUN, call this function to give it to
// Yggdrasil.
func (c *Core) RouterSendPacket(buf []byte) error {
	packet := append(util.GetBytes(), buf[:]...)
	c.router.tun.Send <- packet
	return nil
}
