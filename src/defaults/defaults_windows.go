//go:build windows
// +build windows

package defaults

// Sane defaults for the Windows platform. The "default" options may be
// may be replaced by the running configuration.
func getDefaults() platformDefaultParameters {
	return platformDefaultParameters{
		// Admin
		DefaultAdminListen: "tcp://localhost:9001",

		// Configuration (used for meshctl)
		DefaultConfigFile: "C:\\Program Files\\RiV-mesh\\mesh.conf",

		// Multicast interfaces
		DefaultMulticastInterfaces: []MulticastInterfaceConfig{
			{Regex: ".*", Beacon: true, Listen: true},
		},

		// TUN
		MaximumIfMTU:  65535,
		DefaultIfMTU:  65535,
		DefaultIfName: "RiV-mesh",
	}
}
