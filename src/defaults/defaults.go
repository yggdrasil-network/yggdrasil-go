package defaults

import "github.com/RiV-chain/RiV-mesh/src/config"

type MulticastInterfaceConfig = config.MulticastInterfaceConfig

var defaultConfig = ""      // LDFLAGS='-X github.com/yggdrasil-network/yggdrasil-go/src/defaults.defaultConfig=/path/to/config
var defaultAdminListen = "" // LDFLAGS='-X github.com/yggdrasil-network/yggdrasil-go/src/defaults.defaultAdminListen=unix://path/to/sock'

// Defines which parameters are expected by default for configuration on a
// specific platform. These values are populated in the relevant defaults_*.go
// for the platform being targeted. They must be set.
type platformDefaultParameters struct {
	// Admin socket
	DefaultAdminListen string

	// Configuration (used for meshctl)
	DefaultConfigFile string

	// Multicast interfaces
	DefaultMulticastInterfaces []MulticastInterfaceConfig

	// TUN
	MaximumIfMTU  uint64
	DefaultIfMTU  uint64
	DefaultIfName string
}

func GetDefaults() platformDefaultParameters {
	defaults := getDefaults()
	if defaultConfig != "" {
		defaults.DefaultConfigFile = defaultConfig
	}
	if defaultAdminListen != "" {
		defaults.DefaultAdminListen = defaultAdminListen
	}
	return defaults
}

// Generates default configuration and returns a pointer to the resulting
// NodeConfig. This is used when outputting the -genconf parameter and also when
// using -autoconf.
func GenerateConfig() *config.NodeConfig {
	// Get the defaults for the platform.
	defaults := GetDefaults()
	// Create a node configuration and populate it.
	cfg := new(config.NodeConfig)
	cfg.NewKeys()
	cfg.Listen = []string{}
	cfg.AdminListen = defaults.DefaultAdminListen
	cfg.Peers = []string{}
	cfg.InterfacePeers = map[string][]string{}
	cfg.AllowedPublicKeys = []string{}
	cfg.MulticastInterfaces = defaults.DefaultMulticastInterfaces
	cfg.IfName = defaults.DefaultIfName
	cfg.IfMTU = defaults.DefaultIfMTU
	cfg.NodeInfoPrivacy = false

	return cfg
}
