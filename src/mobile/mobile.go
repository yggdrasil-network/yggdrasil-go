package mobile

import (
	"encoding/json"
	"os"
	"time"

	"github.com/gologme/log"

	hjson "github.com/hjson/hjson-go"
	"github.com/mitchellh/mapstructure"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/dummy"
	"github.com/yggdrasil-network/yggdrasil-go/src/multicast"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

// Yggdrasil mobile package is meant to "plug the gap" for mobile support, as
// Gomobile will not create headers for Swift/Obj-C etc if they have complex
// (non-native) types. Therefore for iOS we will expose some nice simple
// functions. Note that in the case of iOS we handle reading/writing to/from TUN
// in Swift therefore we use the "dummy" TUN interface instead.
type Yggdrasil struct {
	core      yggdrasil.Core
	multicast multicast.Multicast
	log       MobileLogger
	dummy.DummyAdapter
}

func (m *Yggdrasil) addStaticPeers(cfg *config.NodeConfig) {
	if len(cfg.Peers) == 0 && len(cfg.InterfacePeers) == 0 {
		return
	}
	for {
		for _, peer := range cfg.Peers {
			m.core.AddPeer(peer, "")
			time.Sleep(time.Second)
		}
		for intf, intfpeers := range cfg.InterfacePeers {
			for _, peer := range intfpeers {
				m.core.AddPeer(peer, intf)
				time.Sleep(time.Second)
			}
		}
		time.Sleep(time.Minute)
	}
}

// StartAutoconfigure starts a node with a randomly generated config
func (m *Yggdrasil) StartAutoconfigure() error {
	logger := log.New(m.log, "", 0)
	logger.EnableLevel("error")
	logger.EnableLevel("warn")
	logger.EnableLevel("info")
	nc := config.GenerateConfig()
	nc.IfName = "dummy"
	nc.AdminListen = "tcp://localhost:9001"
	nc.Peers = []string{}
	if hostname, err := os.Hostname(); err == nil {
		nc.NodeInfo = map[string]interface{}{"name": hostname}
	}
	if err := m.core.SetRouterAdapter(m); err != nil {
		logger.Errorln("An error occured setting router adapter:", err)
		return err
	}
	state, err := m.core.Start(nc, logger)
	if err != nil {
		logger.Errorln("An error occured starting Yggdrasil:", err)
		return err
	}
	m.multicast.Init(&m.core, state, logger, nil)
	if err := m.multicast.Start(); err != nil {
		logger.Errorln("An error occurred starting multicast:", err)
	}
	go m.addStaticPeers(nc)
	return nil
}

// StartJSON starts a node with the given JSON config. You can get JSON config
// (rather than HJSON) by using the GenerateConfigJSON() function
func (m *Yggdrasil) StartJSON(configjson []byte) error {
	logger := log.New(m.log, "", 0)
	logger.EnableLevel("error")
	logger.EnableLevel("warn")
	logger.EnableLevel("info")
	nc := config.GenerateConfig()
	var dat map[string]interface{}
	if err := hjson.Unmarshal(configjson, &dat); err != nil {
		return err
	}
	if err := mapstructure.Decode(dat, &nc); err != nil {
		return err
	}
	nc.IfName = "dummy"
	if err := m.core.SetRouterAdapter(m); err != nil {
		logger.Errorln("An error occured setting router adapter:", err)
		return err
	}
	state, err := m.core.Start(nc, logger)
	if err != nil {
		logger.Errorln("An error occured starting Yggdrasil:", err)
		return err
	}
	m.multicast.Init(&m.core, state, logger, nil)
	if err := m.multicast.Start(); err != nil {
		logger.Errorln("An error occurred starting multicast:", err)
	}
	go m.addStaticPeers(nc)
	return nil
}

// Stop the mobile Yggdrasil instance
func (m *Yggdrasil) Stop() error {
	m.core.Stop()
	if err := m.Stop(); err != nil {
		return err
	}
	return nil
}

// GenerateConfigJSON generates mobile-friendly configuration in JSON format
func GenerateConfigJSON() []byte {
	nc := config.GenerateConfig()
	nc.IfName = "dummy"
	if json, err := json.Marshal(nc); err == nil {
		return json
	}
	return nil
}

// GetAddressString gets the node's IPv6 address
func (m *Yggdrasil) GetAddressString() string {
	return m.core.Address().String()
}

// GetSubnetString gets the node's IPv6 subnet in CIDR notation
func (m *Yggdrasil) GetSubnetString() string {
	return m.core.Subnet().String()
}

// GetBoxPubKeyString gets the node's public encryption key
func (m *Yggdrasil) GetBoxPubKeyString() string {
	return m.core.BoxPubKey()
}

// GetSigPubKeyString gets the node's public signing key
func (m *Yggdrasil) GetSigPubKeyString() string {
	return m.core.SigPubKey()
}
