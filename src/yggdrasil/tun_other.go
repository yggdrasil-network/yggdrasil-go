// +build !linux,!darwin,!windows,!openbsd,!freebsd,!solaris,!netbsd,!dragonfly

package yggdrasil

import water "github.com/neilalexander/water"

// This is to catch unsupported platforms
// If your platform supports tun devices, you could try configuring it manually

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
	tun.mtu = mtu //1280 // Lets default to the smallest thing allowed for now
	return tun.setupAddress(addr)
}

func (tun *tunDevice) setupAddress(addr string) error {
	tun.core.log.Println("Platform not supported, you must set the address of", tun.iface.Name(), "to", addr)
	return nil
}
