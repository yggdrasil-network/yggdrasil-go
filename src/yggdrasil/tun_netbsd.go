package yggdrasil

// Sane defaults for the NetBSD platform. The "default" options may be
// may be replaced by the running configuration.
func getDefaults() tunDefaultParameters {
	return tunDefaultParameters{
		maximumIfMTU:     9000,
		defaultIfMTU:     9000,
		defaultIfName:    "/dev/tap0",
		defaultIfTAPMode: true,
	}
}
