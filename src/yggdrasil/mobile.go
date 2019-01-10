// +build mobile

package yggdrasil

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"regexp"
	"time"

	hjson "github.com/hjson/hjson-go"
	"github.com/mitchellh/mapstructure"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
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

func (c *Core) StartAutoconfigure() error {
	mobilelog := MobileLogger{}
	logger := log.New(mobilelog, "", 0)
	nc := config.GenerateConfig(true)
	nc.IfName = "dummy"
	nc.AdminListen = "tcp://localhost:9001"
	nc.Peers = []string{}
	if hostname, err := os.Hostname(); err == nil {
		nc.NodeInfo = map[string]interface{}{"name": hostname}
	}
	ifceExpr, err := regexp.Compile(".*")
	if err == nil {
		c.ifceExpr = append(c.ifceExpr, ifceExpr)
	}
	if err := c.Start(nc, logger); err != nil {
		return err
	}
	go c.addStaticPeers(nc)
	return nil
}

func (c *Core) StartJSON(configjson []byte) error {
	mobilelog := MobileLogger{}
	logger := log.New(mobilelog, "", 0)
	nc := config.GenerateConfig(false)
	var dat map[string]interface{}
	if err := hjson.Unmarshal(configjson, &dat); err != nil {
		return err
	}
	if err := mapstructure.Decode(dat, &nc); err != nil {
		return err
	}
	nc.IfName = "dummy"
	//c.log.Println(nc.MulticastInterfaces)
	for _, ll := range nc.MulticastInterfaces {
		//c.log.Println("Processing MC", ll)
		ifceExpr, err := regexp.Compile(ll)
		if err != nil {
			panic(err)
		}
		c.AddMulticastInterfaceExpr(ifceExpr)
	}
	if err := c.Start(nc, logger); err != nil {
		return err
	}
	go c.addStaticPeers(nc)
	return nil
}

func GenerateConfigJSON() []byte {
	nc := config.GenerateConfig(false)
	nc.IfName = "dummy"
	if json, err := json.Marshal(nc); err == nil {
		return json
	} else {
		return nil
	}
}

func (c *Core) GetAddressString() string {
	return c.GetAddress().String()
}

func (c *Core) GetSubnetString() string {
	return c.GetSubnet().String()
}

func (c *Core) RouterRecvPacket() ([]byte, error) {
	packet := <-c.router.tun.recv
	return packet, nil
}

func (c *Core) RouterSendPacket(buf []byte) error {
	packet := append(util.GetBytes(), buf[:]...)
	c.router.tun.send <- packet
	return nil
}

func (c *Core) AWDLCreateInterface(boxPubKey string, sigPubKey string, name string) error {
	fromAWDL := make(chan []byte, 32)
	toAWDL := make(chan []byte, 32)

	var boxPub crypto.BoxPubKey
	var sigPub crypto.SigPubKey
	boxPubHex, err := hex.DecodeString(boxPubKey)
	if err != nil {
		c.log.Println(err)
		return err
	}
	sigPubHex, err := hex.DecodeString(sigPubKey)
	if err != nil {
		c.log.Println(err)
		return err
	}
	copy(boxPub[:], boxPubHex)
	copy(sigPub[:], sigPubHex)

	if intf, err := c.awdl.create(fromAWDL, toAWDL, &boxPub, &sigPub, name); err == nil {
		if intf != nil {
			c.log.Println(err)
			return err
		} else {
			c.log.Println("c.awdl.create didn't return an interface")
			return errors.New("c.awdl.create didn't return an interface")
		}
	} else {
		c.log.Println(err)
		return err
	}
}

func (c *Core) AWDLCreateInterfaceFromContext(context []byte, name string) error {
	if len(context) < crypto.BoxPubKeyLen+crypto.SigPubKeyLen {
		return errors.New("Not enough bytes in context")
	}
	boxPubKey := hex.EncodeToString(context[:crypto.BoxPubKeyLen])
	sigPubKey := hex.EncodeToString(context[crypto.BoxPubKeyLen:])
	return c.AWDLCreateInterface(boxPubKey, sigPubKey, name)
}

func (c *Core) AWDLShutdownInterface(name string) error {
	return c.awdl.shutdown(name)
}

func (c *Core) AWDLRecvPacket(identity string) ([]byte, error) {
	if intf := c.awdl.getInterface(identity); intf != nil {
		return <-intf.toAWDL, nil
	}
	return nil, errors.New("AWDLRecvPacket identity not known: " + identity)
}

func (c *Core) AWDLSendPacket(identity string, buf []byte) error {
	packet := append(util.GetBytes(), buf[:]...)
	if intf := c.awdl.getInterface(identity); intf != nil {
		intf.fromAWDL <- packet
		return nil
	}
	return errors.New("AWDLSendPacket identity not known: " + identity)
}

func (c *Core) AWDLConnectionContext() []byte {
	var context []byte
	context = append(context, c.boxPub[:]...)
	context = append(context, c.sigPub[:]...)
	return context
}
