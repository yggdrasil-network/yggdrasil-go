// +build !linux
// +build !darwin

package yggdrasil

import water "github.com/songgao/water"

// This is to catch unsupported platforms
// If your platform supports tun devices, you could try configuring it manually

func (tun *tunDevice) setup(ifname string, addr string, mtu int) error {
	config := water.Config{DeviceType: water.TUN}
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
