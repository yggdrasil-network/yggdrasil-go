// +build darwin

package defaults

// Sane defaults for the macOS/Darwin platform. The "default" options may be
// may be replaced by the running configuration.
func GetDefaults() platformDefaultParameters {
	return platformDefaultParameters{
		// Admin
		DefaultAdminListen: "unix:///var/run/yggdrasil.sock",

		// TUN/TAP
		MaximumIfMTU:     65535,
		DefaultIfMTU:     65535,
		DefaultIfName:    "auto",
		DefaultIfTAPMode: false,
	}
}
