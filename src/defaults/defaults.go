package defaults

import "github.com/yggdrasil-network/yggdrasil-go/src/config"

type MulticastInterfaceConfig = config.MulticastInterfaceConfig

// Defines which parameters are expected by default for configuration on a
// specific platform. These values are populated in the relevant defaults_*.go
// for the platform being targeted. They must be set.
type platformDefaultParameters struct {
	// Admin socket
	DefaultAdminListen string

	// Configuration (used for yggdrasilctl)
	DefaultConfigFile string

	// Multicast interfaces
	DefaultMulticastInterfaces []MulticastInterfaceConfig

	// TUN/TAP
	MaximumIfMTU  uint64
	DefaultIfMTU  uint64
	DefaultIfName string
}

// Generates default configuration and returns a pointer to the resulting
// NodeConfig. This is used when outputting the -genconf parameter and also when
// using -autoconf.
func GenerateConfig() *config.NodeConfig {
	// Create a node configuration and populate it.
	cfg := new(config.NodeConfig)
	cfg.NewKeys()
	cfg.Listen = []string{}
	cfg.AdminListen = GetDefaults().DefaultAdminListen
	cfg.Peers = []string{}
	cfg.InterfacePeers = map[string][]string{}
	cfg.AllowedPublicKeys = []string{}
	cfg.MulticastInterfaces = GetDefaults().DefaultMulticastInterfaces
	cfg.IfName = GetDefaults().DefaultIfName
	cfg.IfMTU = GetDefaults().DefaultIfMTU
	cfg.NodeInfoPrivacy = false

	return cfg
}
