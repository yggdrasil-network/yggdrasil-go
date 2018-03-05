package yggdrasil

func getDefaults() tunDefaultParameters {
	return tunDefaultParameters{
		maximumIfMTU:     32767,
		defaultIfMTU:     32767,
		defaultIfName:    "/dev/tap0",
		defaultIfTAPMode: true,
	}
}
