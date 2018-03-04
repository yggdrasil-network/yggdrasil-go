package yggdrasil

func getDefaults() tunDefaultParameters {
	return tunDefaultParameters{
		maximumIfMTU:     9000,
		defaultIfMTU:     9000,
		defaultIfName:    "/dev/tap0",
		defaultIfTAPMode: true,
	}
}
