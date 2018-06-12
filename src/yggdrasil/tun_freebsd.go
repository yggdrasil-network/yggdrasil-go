package yggdrasil

// Sane defaults for the FreeBSD platform. The "default" options may be
// may be replaced by the running configuration.
func getDefaults() tunDefaultParameters {
	return tunDefaultParameters{
		maximumIfMTU:     32767,
		defaultIfMTU:     32767,
		defaultIfName:    "/dev/tap0",
		defaultIfTAPMode: true,
	}
}
