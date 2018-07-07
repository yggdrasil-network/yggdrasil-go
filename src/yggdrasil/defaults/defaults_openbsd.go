// +build openbsd

package defaults

// Sane defaults for the BSD platforms. The "default" options may be
// may be replaced by the running configuration.
func GetDefaults() platformDefaultParameters {
	return platformDefaultParameters{
		// Admin
		DefaultAdminListen: "tcp://localhost:9001",

		// TUN/TAP
		MaximumIfMTU:     16384,
		DefaultIfMTU:     16384,
		DefaultIfName:    "/dev/tap0",
		DefaultIfTAPMode: true,
	}
}
