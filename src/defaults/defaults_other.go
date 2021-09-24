//go:build !linux && !darwin && !windows && !openbsd && !freebsd
// +build !linux,!darwin,!windows,!openbsd,!freebsd

package defaults

// Sane defaults for the other platforms. The "default" options may be
// may be replaced by the running configuration.
func GetDefaults() platformDefaultParameters {
	return platformDefaultParameters{
		// Admin
		DefaultAdminListen: "tcp://localhost:9001",

		// Configuration (used for yggdrasilctl)
		DefaultConfigFile: "/etc/yggdrasil.conf",

		// Multicast interfaces
		DefaultMulticastInterfaces: []MulticastInterfaceConfig{
			{Regex: ".*", Beacon: true, Listen: true},
		},

		// TUN/TAP
		MaximumIfMTU:  65535,
		DefaultIfMTU:  65535,
		DefaultIfName: "none",
	}
}
