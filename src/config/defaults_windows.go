//go:build windows
// +build windows

package config

// Sane defaults for the Windows platform. The "default" options may be
// may be replaced by the running configuration.
func getDefaults() platformDefaultParameters {
	return platformDefaultParameters{
		// Admin
		DefaultAdminListen: "tcp://localhost:9001",

		// Configuration (used for yggdrasilctl)
		DefaultConfigFile: "C:\\Program Files\\Yggdrasil\\yggdrasil.conf",

		// Multicast interfaces
		DefaultMulticastInterfaces: []MulticastInterfaceConfig{
			{
				Regex:    ".*",
				Beacon:   true,
				Listen:   true,
				Port:     0,  // 0 means random port
				Priority: 0,  // 0 is highest priority
				Password: "", // empty means no password required
			},
		},

		// TUN
		MaximumIfMTU:  65535,
		DefaultIfMTU:  65535,
		DefaultIfName: "Yggdrasil",
	}
}
