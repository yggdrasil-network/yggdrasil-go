// +build netbsd

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
		DefaultMulticastInterfaces: []string{
			".*",
		},

		// TUN/TAP
		MaximumIfMTU:     9000,
		DefaultIfMTU:     9000,
		DefaultIfName:    "/dev/tap0",
		DefaultIfTAPMode: true,
	}
}
