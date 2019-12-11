package defaults

// Defines which parameters are expected by default for configuration on a
// specific platform. These values are populated in the relevant defaults_*.go
// for the platform being targeted. They must be set.
type platformDefaultParameters struct {
	// Admin socket
	DefaultAdminListen string

	// Configuration (used for yggdrasilctl)
	DefaultConfigFile string

	// Multicast interfaces
	DefaultMulticastInterfaces []string

	// TUN/TAP
	MaximumIfMTU  int
	DefaultIfMTU  int
	DefaultIfName string
}
