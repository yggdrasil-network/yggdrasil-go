// +build !linux,!darwin,!windows,!openbsd,!freebsd,!netbsd

package yggdrasil

import water "github.com/yggdrasil-network/water"

// This is to catch unsupported platforms
// If your platform supports tun devices, you could try configuring it manually

// These are sane defaults for any platform that has not been matched by one of
// the other tun_*.go files.
func getDefaults() tunDefaultParameters {
	return tunDefaultParameters{
		maximumIfMTU:     65535,
		defaultIfMTU:     65535,
		defaultIfName:    "none",
		defaultIfTAPMode: false,
	}
}

// Creates the TUN/TAP adapter, if supported by the Water library. Note that
// no guarantees are made at this point on an unsupported platform.
func (tun *tunDevice) setup(ifname string, iftapmode bool, addr string, mtu int) error {
	var config water.Config
	if iftapmode {
		config = water.Config{DeviceType: water.TAP}
	} else {
		config = water.Config{DeviceType: water.TUN}
	}
	iface, err := water.New(config)
	if err != nil {
		panic(err)
	}
	tun.iface = iface
	tun.mtu = getSupportedMTU(mtu)
	return tun.setupAddress(addr)
}

// We don't know how to set the IPv6 address on an unknown platform, therefore
// write about it to stdout and don't try to do anything further.
func (tun *tunDevice) setupAddress(addr string) error {
	tun.core.log.Println("Platform not supported, you must set the address of", tun.iface.Name(), "to", addr)
	return nil
}
