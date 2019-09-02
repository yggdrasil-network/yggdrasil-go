package config

import (
	"encoding/hex"
	"encoding/json"

	hjson "github.com/hjson/hjson-go"
	"github.com/mitchellh/mapstructure"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
)

// Generates default configuration. This is used when outputting the -genconf
// parameter and also when using -autoconf.
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
	bs, err := json.MarshalIndent(*cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return bs, nil
}

// MarshalHJSON exports the configuration into HJSON format, complete with
// comments describing what each configuration item does.
func (cfg *NodeConfig) MarshalHJSON() ([]byte, error) {
	bs, err := hjson.Marshal(*cfg)
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
	// Overlay our newly mapped configuration onto the NodeConfig
	if err := mapstructure.Decode(dat, &cfg); err != nil {
		return err
	}
	return nil
}
