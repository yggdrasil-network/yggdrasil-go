//go:build openbsd
// +build openbsd

package defaults

// Sane defaults for the BSD platforms. The "default" options may be
// may be replaced by the running configuration.
func GetDefaults() platformDefaultParameters {
	return platformDefaultParameters{
		// Admin
		DefaultAdminListen: "unix:///var/run/yggdrasil.sock",

		// Configuration (used for yggdrasilctl)
		DefaultConfigFile: "/etc/yggdrasil.conf",

		// Multicast interfaces
		DefaultMulticastInterfaces: []MulticastInterfaceConfig{
			{Regex: ".*", Beacon: true, Listen: true},
		},

		// TUN/TAP
		MaximumIfMTU:  16384,
		DefaultIfMTU:  16384,
		DefaultIfName: "tun0",
	}
}
