//go:build !mobile
// +build !mobile

package tuntap

// The linux platform specific tun parts

import (
	"github.com/vishvananda/netlink"
	wgtun "golang.zx2c4.com/wireguard/tun"
)

// Configures the TUN adapter with the correct IPv6 address and MTU.
func (tun *TunAdapter) setup(ifname string, addr string, mtu uint64) error {
	if ifname == "auto" {
		ifname = "\000"
	}
	iface, err := wgtun.CreateTUN(ifname, int(mtu))
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

// Configures the TAP adapter with the correct IPv6 address and MTU. Netlink
// is used to do this, so there is not a hard requirement on "ip" or "ifconfig"
// to exist on the system, but this will fail if Netlink is not present in the
// kernel (it nearly always is).
func (tun *TunAdapter) setupAddress(addr string) error {
	nladdr, err := netlink.ParseAddr(addr)
	if err != nil {
		return err
	}
	nlintf, err := netlink.LinkByName(tun.Name())
	if err != nil {
		return err
	}
	if err := netlink.AddrAdd(nlintf, nladdr); err != nil {
		return err
	}
	if err := netlink.LinkSetMTU(nlintf, int(tun.mtu)); err != nil {
		return err
	}
	if err := netlink.LinkSetUp(nlintf); err != nil {
		return err
	}
	// Friendly output
	tun.log.Infof("Interface name: %s", tun.Name())
	tun.log.Infof("Interface IPv6: %s", addr)
	tun.log.Infof("Interface MTU: %d", tun.mtu)
	return nil
}
