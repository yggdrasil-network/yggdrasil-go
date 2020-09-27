// +build freebsd

package defaults

// Sane defaults for the BSD platforms. The "default" options may be
// may be replaced by the running configuration.
func GetDefaults() platformDefaultParameters {
	return platformDefaultParameters{
		// Admin
		DefaultAdminListen: "unix:///var/run/yggdrasil.sock",

		// Configuration (used for yggdrasilctl)
		DefaultConfigFile: "/usr/local/etc/yggdrasil.conf",

		// Multicast interfaces
		DefaultMulticastInterfaces: []string{
			".*",
		},

		// TUN/TAP
		MaximumIfMTU:  32767,
		DefaultIfMTU:  32767,
		DefaultIfName: "/dev/tun0",
	}
}
