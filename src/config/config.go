/*
The config package contains structures related to the configuration of an
Yggdrasil node.

The configuration contains, amongst other things, encryption keys which are used
to derive a node's identity, information about peerings and node information
that is shared with the network. There are also some module-specific options
related to TUN, multicast and the admin socket.

In order for a node to maintain the same identity across restarts, you should
persist the configuration onto the filesystem or into some configuration storage
so that the encryption keys (and therefore the node ID) do not change.

Note that Yggdrasil will automatically populate sane defaults for any
configuration option that is not provided.
*/
package config

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/hjson/hjson-go"
	"github.com/mitchellh/mapstructure"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
)

// NodeConfig is the main configuration structure, containing configuration
// options that are necessary for an Yggdrasil node to run. You will need to
// supply one of these structs to the Yggdrasil core when starting a node.
type NodeConfig struct {
	sync.RWMutex        `json:"-"`
	Peers               []string                   `comment:"List of connection strings for outbound peer connections in URI format,\ne.g. tls://a.b.c.d:e or socks://a.b.c.d:e/f.g.h.i:j. These connections\nwill obey the operating system routing table, therefore you should\nuse this section when you may connect via different interfaces."`
	InterfacePeers      map[string][]string        `comment:"List of connection strings for outbound peer connections in URI format,\narranged by source interface, e.g. { \"eth0\": [ tls://a.b.c.d:e ] }.\nNote that SOCKS peerings will NOT be affected by this option and should\ngo in the \"Peers\" section instead."`
	Listen              []string                   `comment:"Listen addresses for incoming connections. You will need to add\nlisteners in order to accept incoming peerings from non-local nodes.\nMulticast peer discovery will work regardless of any listeners set\nhere. Each listener should be specified in URI format as above, e.g.\ntls://0.0.0.0:0 or tls://[::]:0 to listen on all interfaces."`
	AdminListen         string                     `comment:"Listen address for admin connections. Default is to listen for local\nconnections either on TCP/9001 or a UNIX socket depending on your\nplatform. Use this value for yggdrasilctl -endpoint=X. To disable\nthe admin socket, use the value \"none\" instead."`
	MulticastInterfaces []MulticastInterfaceConfig `comment:"Configuration for which interfaces multicast peer discovery should be\nenabled on. Each entry in the list should be a json object which may\ncontain Regex, Beacon, Listen, and Port. Regex is a regular expression\nwhich is matched against an interface name, and interfaces use the\nfirst configuration that they match gainst. Beacon configures whether\nor not the node should send link-local multicast beacons to advertise\ntheir presence, while listening for incoming connections on Port.\nListen controls whether or not the node listens for multicast beacons\nand opens outgoing connections."`
	AllowedPublicKeys   []string                   `comment:"List of peer public keys to allow incoming peering connections\nfrom. If left empty/undefined then all connections will be allowed\nby default. This does not affect outgoing peerings, nor does it\naffect link-local peers discovered via multicast."`
	PublicKey           string                     `comment:"Your public key. Your peers may ask you for this to put\ninto their AllowedPublicKeys configuration."`
	PrivateKey          string                     `comment:"Your private key. DO NOT share this with anyone!"`
	IfName              string                     `comment:"Local network interface name for TUN adapter, or \"auto\" to select\nan interface automatically, or \"none\" to run without TUN."`
	IfMTU               uint64                     `comment:"Maximum Transmission Unit (MTU) size for your local TUN interface.\nDefault is the largest supported size for your platform. The lowest\npossible value is 1280."`
	NodeInfoPrivacy     bool                       `comment:"By default, nodeinfo contains some defaults including the platform,\narchitecture and Yggdrasil version. These can help when surveying\nthe network and diagnosing network routing problems. Enabling\nnodeinfo privacy prevents this, so that only items specified in\n\"NodeInfo\" are sent back if specified."`
	NodeInfo            map[string]interface{}     `comment:"Optional node info. This must be a { \"key\": \"value\", ... } map\nor set as null. This is entirely optional but, if set, is visible\nto the whole network on request."`
}

type MulticastInterfaceConfig struct {
	Regex  string
	Beacon bool
	Listen bool
	Port   uint16
}

// NewSigningKeys replaces the signing keypair in the NodeConfig with a new
// signing keypair. The signing keys are used by the switch to derive the
// structure of the spanning tree.
func (cfg *NodeConfig) NewKeys() {
	spub, spriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}
	cfg.PublicKey = hex.EncodeToString(spub[:])
	cfg.PrivateKey = hex.EncodeToString(spriv[:])
}

// Generates default configuration and returns a pointer to the resulting
// NodeConfig. This is used when outputting the -genconf parameter and also when
// using -autoconf.
func GenerateConfig() *NodeConfig {
	defaults := defaults.GetDefaults()
	cfg := &NodeConfig{}
	cfg.NewKeys()
	cfg.Listen = []string{}
	cfg.AdminListen = defaults.DefaultAdminListen
	cfg.Peers = []string{}
	cfg.InterfacePeers = map[string][]string{}
	cfg.AllowedPublicKeys = []string{}
	for _, regex := range defaults.DefaultMulticastInterfaces {
		cfg.MulticastInterfaces = append(cfg.MulticastInterfaces, MulticastInterfaceConfig{
			Regex:  regex,
			Beacon: true,
			Listen: true,
		})
	}
	cfg.IfName = defaults.DefaultIfName
	cfg.IfMTU = defaults.DefaultIfMTU
	cfg.NodeInfoPrivacy = false
	return cfg
}

func GenerateConfigJSON(isjson bool) []byte {
	// Generates a new configuration and returns it in HJSON or JSON format.
	cfg := GenerateConfig()
	var bs []byte
	var err error
	if isjson {
		bs, err = json.MarshalIndent(cfg, "", "  ")
	} else {
		bs, err = hjson.Marshal(cfg)
	}
	if err != nil {
		panic(err)
	}
	return bs
}

func ReadConfig(conf []byte) *NodeConfig {
	// Generate a new configuration - this gives us a set of sane defaults -
	// then parse the configuration we loaded above on top of it. The effect
	// of this is that any configuration item that is missing from the provided
	// configuration will use a sane default.
	cfg := GenerateConfig()
	var dat map[string]interface{}
	if err := hjson.Unmarshal(conf, &dat); err != nil {
		panic(err)
	}
	// Check if we have old field names
	if old, ok := dat["SigningPrivateKey"]; ok {
		if _, ok := dat["PrivateKey"]; !ok {
			if privstr, err := hex.DecodeString(old.(string)); err == nil {
				priv := ed25519.PrivateKey(privstr)
				pub := priv.Public().(ed25519.PublicKey)
				dat["PrivateKey"] = hex.EncodeToString(priv[:])
				dat["PublicKey"] = hex.EncodeToString(pub[:])
			}
		}
	}
	if oldmc, ok := dat["MulticastInterfaces"]; ok {
		if oldmcvals, ok := oldmc.([]interface{}); ok {
			var newmc []MulticastInterfaceConfig
			for _, oldmcval := range oldmcvals {
				if str, ok := oldmcval.(string); ok {
					newmc = append(newmc, MulticastInterfaceConfig{
						Regex:  str,
						Beacon: true,
						Listen: true,
					})
				}
			}
			if newmc != nil {
				if oldport, ok := dat["LinkLocalTCPPort"]; ok {
					// numbers parse to float64 by default
					if port, ok := oldport.(float64); ok {
						for idx := range newmc {
							newmc[idx].Port = uint16(port)
						}
					}
				}
				dat["MulticastInterfaces"] = newmc
			}
		}
	}
	// Sanitise the config
	confJson, err := json.Marshal(dat)
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(confJson, &cfg); err != nil {
		panic(err)
	}
	// Overlay our newly mapped configuration onto the autoconf node config that
	// we generated above.
	if err = mapstructure.Decode(dat, &cfg); err != nil {
		panic(err)
	}
	return cfg
}
