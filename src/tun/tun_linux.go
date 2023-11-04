//go:build linux || android
// +build linux android

package tun

// The linux platform specific tun parts

import (
	"fmt"

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
		return fmt.Errorf("failed to create TUN: %w", err)
	}
	tun.iface = iface
	if mtu, err := iface.MTU(); err == nil {
		tun.mtu = getSupportedMTU(uint64(mtu))
	} else {
		tun.mtu = 0
	}
	if addr != "" {
		return tun.setupAddress(addr)
	}
	return nil
}

// Configures the "utun" adapter from an existing file descriptor.
func (tun *TunAdapter) setupFD(fd int32, addr string, mtu uint64) error {
	return fmt.Errorf("setup via FD not supported on this platform")
}

// Configures the TUN adapter with the correct IPv6 address and MTU. Netlink
// is used to do this, so there is not a hard requirement on "ip" or "ifconfig"
// to exist on the system, but this will fail if Netlink is not present in the
// kernel (it nearly always is).
func (tun *TunAdapter) setupAddress(addr string) error {
	nladdr, err := netlink.ParseAddr(addr)
	if err != nil {
		return fmt.Errorf("couldn't parse address %q: %w", addr, err)
	}
	nlintf, err := netlink.LinkByName(tun.Name())
	if err != nil {
		return fmt.Errorf("failed to find link by name: %w", err)
	}
	if err := netlink.AddrAdd(nlintf, nladdr); err != nil {
		return fmt.Errorf("failed to add address to link: %w", err)
	}
	if err := netlink.LinkSetMTU(nlintf, int(tun.mtu)); err != nil {
		return fmt.Errorf("failed to set link MTU: %w", err)
	}
	if err := netlink.LinkSetUp(nlintf); err != nil {
		return fmt.Errorf("failed to bring link up: %w", err)
	}
	// Friendly output
	tun.log.Infof("Interface name: %s", tun.Name())
	tun.log.Infof("Interface IPv6: %s", addr)
	tun.log.Infof("Interface MTU: %d", tun.mtu)
	return nil
}
