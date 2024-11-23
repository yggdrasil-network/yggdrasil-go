//go:build darwin
// +build darwin

package config

// Sane defaults for the macOS/Darwin platform. The "default" options may be
// may be replaced by the running configuration.
func getDefaults() platformDefaultParameters {
	return platformDefaultParameters{
		// Admin
		DefaultAdminListen: "unix:///var/run/yggdrasil.sock",

		// Configuration (used for yggdrasilctl)
		DefaultConfigFile: "/etc/yggdrasil.conf",

		// Multicast interfaces
		DefaultMulticastInterfaces: []MulticastInterfaceConfig{
			{Regex: "en.*", Beacon: true, Listen: true},
			{Regex: "bridge.*", Beacon: true, Listen: true},
			{Regex: "awdl0", Beacon: false, Listen: false},
		},

		// TUN
		MaximumIfMTU:  65535,
		DefaultIfMTU:  65535,
		DefaultIfName: "auto",
	}
}
