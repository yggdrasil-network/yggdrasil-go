//go:build !linux && !darwin && !windows && !openbsd && !freebsd && !mobile
// +build !linux,!darwin,!windows,!openbsd,!freebsd,!mobile

package tuntap

// This is to catch unsupported platforms
// If your platform supports tun devices, you could try configuring it manually

import (
	wgtun "golang.zx2c4.com/wireguard/tun"
)

// Configures the TUN adapter with the correct IPv6 address and MTU.
func (tun *TunAdapter) setup(ifname string, addr string, mtu uint64) error {
	iface, err := wgtun.CreateTUN(ifname, mtu)
	if err != nil {
		panic(err)
	}
	tun.iface = iface
	if mtu, err := iface.MTU(); err == nil {
		tun.mtu = getSupportedMTU(uint64(mtu))
	} else {
		tun.mtu = 0
	}
	return tun.setupAddress(addr)
}

// We don't know how to set the IPv6 address on an unknown platform, therefore
// write about it to stdout and don't try to do anything further.
func (tun *TunAdapter) setupAddress(addr string) error {
	tun.log.Warnln("Warning: Platform not supported, you must set the address of", tun.Name(), "to", addr)
	return nil
}
