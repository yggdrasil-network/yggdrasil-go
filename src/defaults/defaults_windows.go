// +build windows

package defaults

// Sane defaults for the Windows platform. The "default" options may be
// may be replaced by the running configuration.
func GetDefaults() platformDefaultParameters {
	return platformDefaultParameters{
		// Admin
		DefaultAdminListen: "tcp://localhost:9001",

		// Configuration (used for yggdrasilctl)
		DefaultConfigFile: "C:\\Program Files\\Yggdrasil\\yggdrasil.conf",

		// Multicast interfaces
		DefaultMulticastInterfaces: []string{
			".*",
		},

		// TUN/TAP
		MaximumIfMTU:     65535,
		DefaultIfMTU:     65535,
		DefaultIfName:    "auto",
		DefaultIfTAPMode: true,
	}
}
