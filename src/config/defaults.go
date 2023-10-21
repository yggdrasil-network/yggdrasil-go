package config

var defaultConfig = ""      // LDFLAGS='-X github.com/yggdrasil-network/yggdrasil-go/src/config.defaultConfig=/path/to/config
var defaultAdminListen = "" // LDFLAGS='-X github.com/yggdrasil-network/yggdrasil-go/src/config.defaultAdminListen=unix://path/to/sock'

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
