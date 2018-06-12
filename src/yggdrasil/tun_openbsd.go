package yggdrasil

// Sane defaults for the OpenBSD platform. The "default" options may be
// may be replaced by the running configuration.
func getDefaults() tunDefaultParameters {
	return tunDefaultParameters{
		maximumIfMTU:     16384,
		defaultIfMTU:     16384,
		defaultIfName:    "/dev/tap0",
		defaultIfTAPMode: true,
	}
}
