package config

import (
	"encoding/hex"
	"encoding/json"

	hjson "github.com/hjson/hjson-go"
	"github.com/mitchellh/mapstructure"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
)

// NodeConfig defines all configuration values needed to run a signle yggdrasil node
type NodeConfig struct {
	Peers                       []string            `comment:"List of connection strings for outbound peer connections in URI format,\ne.g. tcp://a.b.c.d:e or socks://a.b.c.d:e/f.g.h.i:j. These connections\nwill obey the operating system routing table, therefore you should\nuse this section when you may connect via different interfaces."`
	InterfacePeers              map[string][]string `comment:"List of connection strings for outbound peer connections in URI format,\narranged by source interface, e.g. { \"eth0\": [ tcp://a.b.c.d:e ] }.\nNote that SOCKS peerings will NOT be affected by this option and should\ngo in the \"Peers\" section instead."`
	Listen                      []string            `comment:"Listen addresses for incoming connections. You will need to add\nlisteners in order to accept incoming peerings from non-local nodes.\nMulticast peer discovery will work regardless of any listeners set\nhere. Each listener should be specified in URI format as above, e.g.\ntcp://0.0.0.0:0 or tcp://[::]:0 to listen on all interfaces."`
	AdminConfig                 ``
	MulticastConfig             ``
	AllowedEncryptionPublicKeys []string `comment:"List of peer encryption public keys to allow incoming TCP peering\nconnections from. If left empty/undefined then all connections will\nbe allowed by default. This does not affect outgoing peerings, nor\ndoes it affect link-local peers discovered via multicast."`
	EncryptionPublicKey         string   `comment:"Your public encryption key. Your peers may ask you for this to put\ninto their AllowedEncryptionPublicKeys configuration."`
	EncryptionPrivateKey        string   `comment:"Your private encryption key. DO NOT share this with anyone!"`
	SigningPublicKey            string   `comment:"Your public signing key. You should not ordinarily need to share\nthis with anyone."`
	SigningPrivateKey           string   `comment:"Your private signing key. DO NOT share this with anyone!"`
	LinkLocalTCPPort            uint16   `comment:"The port number to be used for the link-local TCP listeners for the\nconfigured MulticastInterfaces. This option does not affect listeners\nspecified in the Listen option. Unless you plan to firewall link-local\ntraffic, it is best to leave this as the default value of 0. This\noption cannot currently be changed by reloading config during runtime."`
	TunTapConfig                ``
	SessionFirewall             SessionFirewall        `comment:"The session firewall controls who can send/receive network traffic\nto/from. This is useful if you want to protect this node without\nresorting to using a real firewall. This does not affect traffic\nbeing routed via this node to somewhere else. Rules are prioritised as\nfollows: blacklist, whitelist, always allow outgoing, direct, remote."`
	SwitchOptions               SwitchOptions          `comment:"Advanced options for tuning the switch. Normally you will not need\nto edit these options."`
	NodeInfoPrivacy             bool                   `comment:"By default, nodeinfo contains some defaults including the platform,\narchitecture and Yggdrasil version. These can help when surveying\nthe network and diagnosing network routing problems. Enabling\nnodeinfo privacy prevents this, so that only items specified in\n\"NodeInfo\" are sent back if specified."`
	NodeInfo                    map[string]interface{} `comment:"Optional node info. This must be a { \"key\": \"value\", ... } map\nor set as null. This is entirely optional but, if set, is visible\nto the whole network on request."`
}

// SessionFirewall controls the session firewall configuration
type SessionFirewall struct {
	Enable                        bool     `comment:"Enable or disable the session firewall. If disabled, network traffic\nfrom any node will be allowed. If enabled, the below rules apply."`
	AllowFromDirect               bool     `comment:"Allow network traffic from directly connected peers."`
	AllowFromRemote               bool     `comment:"Allow network traffic from remote nodes on the network that you are\nnot directly peered with."`
	AlwaysAllowOutbound           bool     `comment:"Allow outbound network traffic regardless of AllowFromDirect or\nAllowFromRemote. This does allow a remote node to send unsolicited\ntraffic back to you for the length of the session."`
	WhitelistEncryptionPublicKeys []string `comment:"List of public keys from which network traffic is always accepted,\nregardless of AllowFromDirect or AllowFromRemote."`
	BlacklistEncryptionPublicKeys []string `comment:"List of public keys from which network traffic is always rejected,\nregardless of the whitelist, AllowFromDirect or AllowFromRemote."`
}

// SwitchOptions contains tuning options for the switch
type SwitchOptions struct {
	MaxTotalQueueSize uint64 `comment:"Maximum size of all switch queues combined (in bytes)."`
}

// Generates default configuration. This is used when outputting the -genconf
// parameter and also when using -autoconf. The isAutoconf flag is used to
// determine whether the operating system should select a free port by itself
// (which guarantees that there will not be a conflict with any other services)
// or whether to generate a random port number. The only side effect of setting
// isAutoconf is that the TCP and UDP ports will likely end up with different
// port numbers.
func GenerateConfig() *NodeConfig {
	// Generate encryption keys.
	bpub, bpriv := crypto.NewBoxKeys()
	spub, spriv := crypto.NewSigKeys()
	// Create a node configuration and populate it.
	cfg := NodeConfig{}
	cfg.Listen = []string{}
	cfg.AdminListen = defaults.GetDefaults().DefaultAdminListen
	cfg.EncryptionPublicKey = hex.EncodeToString(bpub[:])
	cfg.EncryptionPrivateKey = hex.EncodeToString(bpriv[:])
	cfg.SigningPublicKey = hex.EncodeToString(spub[:])
	cfg.SigningPrivateKey = hex.EncodeToString(spriv[:])
	cfg.Peers = []string{}
	cfg.InterfacePeers = map[string][]string{}
	cfg.AllowedEncryptionPublicKeys = []string{}
	cfg.MulticastInterfaces = defaults.GetDefaults().DefaultMulticastInterfaces
	cfg.IfName = defaults.GetDefaults().DefaultIfName
	cfg.IfMTU = defaults.GetDefaults().DefaultIfMTU
	cfg.IfTAPMode = defaults.GetDefaults().DefaultIfTAPMode
	cfg.SessionFirewall.Enable = false
	cfg.SessionFirewall.AllowFromDirect = true
	cfg.SessionFirewall.AllowFromRemote = true
	cfg.SessionFirewall.AlwaysAllowOutbound = true
	cfg.SwitchOptions.MaxTotalQueueSize = 4 * 1024 * 1024
	cfg.NodeInfoPrivacy = false

	return &cfg
}

// NewEncryptionKeys generates a new encryption keypair. The encryption keys are
// used to encrypt traffic and to derive the IPv6 address/subnet of the node.
func (cfg *NodeConfig) NewEncryptionKeys() {
	bpub, bpriv := crypto.NewBoxKeys()
	cfg.EncryptionPublicKey = hex.EncodeToString(bpub[:])
	cfg.EncryptionPrivateKey = hex.EncodeToString(bpriv[:])
}

// NewSigningKeys generates a new signing keypair. The signing keys are used to
// derive the structure of the spanning tree.
func (cfg *NodeConfig) NewSigningKeys() {
	spub, spriv := crypto.NewSigKeys()
	cfg.SigningPublicKey = hex.EncodeToString(spub[:])
	cfg.SigningPrivateKey = hex.EncodeToString(spriv[:])
}

// MarshalJSON exports the configuration into JSON format. No comments are
// included in the JSON export as comments are not valid in pure JSON.
func (cfg *NodeConfig) MarshalJSON() ([]byte, error) {
	bs, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return bs, nil
}

// MarshalHJSON exports the configuration into HJSON format, complete with
// comments describing what each configuration item does.
func (cfg *NodeConfig) MarshalHJSON() ([]byte, error) {
	bs, err := hjson.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return bs, nil
}

// UnmarshalJSON parses the configuration in pure JSON format and updates the
// NodeConfig accordingly. The input JSON can be partial - only supplied fields
// will be updated.
func (cfg *NodeConfig) UnmarshalJSON(conf []byte) error {
	var dat map[string]interface{}
	if err := json.Unmarshal(conf, &dat); err != nil {
		return err
	}
	return cfg.decodeConfig(dat)
}

// UnmarshalHJSON parses the configuration in HJSON format and updates the
// NodeConfig accordingly. The input HJSON can be partial - only supplied fields
// will be updated.
func (cfg *NodeConfig) UnmarshalHJSON(conf []byte) error {
	var dat map[string]interface{}
	if err := hjson.Unmarshal(conf, &dat); err != nil {
		return err
	}
	return cfg.decodeConfig(dat)
}

func (cfg *NodeConfig) decodeConfig(dat map[string]interface{}) error {
	// Check for fields that have changed type recently, e.g. the Listen config
	// option is now a []string rather than a string
	if listen, ok := dat["Listen"].(string); ok {
		dat["Listen"] = []string{listen}
	}
	if tunnelrouting, ok := dat["TunnelRouting"].(map[string]interface{}); ok {
		if c, ok := tunnelrouting["IPv4Sources"]; ok {
			delete(tunnelrouting, "IPv4Sources")
			tunnelrouting["IPv4LocalSubnets"] = c
		}
		if c, ok := tunnelrouting["IPv6Sources"]; ok {
			delete(tunnelrouting, "IPv6Sources")
			tunnelrouting["IPv6LocalSubnets"] = c
		}
		if c, ok := tunnelrouting["IPv4Destinations"]; ok {
			delete(tunnelrouting, "IPv4Destinations")
			tunnelrouting["IPv4RemoteSubnets"] = c
		}
		if c, ok := tunnelrouting["IPv6Destinations"]; ok {
			delete(tunnelrouting, "IPv6Destinations")
			tunnelrouting["IPv6RemoteSubnets"] = c
		}
	}
	// Sanitise the config
	/*confJson, err := json.Marshal(dat)
	if err != nil {
		return err
	}
	json.Unmarshal(confJson, &cfg)*/
	// Overlay our newly mapped configuration onto the autoconf node config that
	// we generated above.
	if err := mapstructure.Decode(dat, &cfg); err != nil {
		return err
	}
	return nil
}
