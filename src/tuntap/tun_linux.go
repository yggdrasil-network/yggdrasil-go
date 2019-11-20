// +build !mobile

package tuntap

// The linux platform specific tun parts

import (
	"github.com/vishvananda/netlink"

	water "github.com/yggdrasil-network/water"
)

// Configures the TAP adapter with the correct IPv6 address and MTU.
func (tun *TunAdapter) setup(ifname string, iftapmode bool, addr string, mtu int) error {
	var config water.Config
	if iftapmode {
		config = water.Config{DeviceType: water.TAP}
	} else {
		config = water.Config{DeviceType: water.TUN}
	}
	if ifname != "" && ifname != "auto" {
		config.Name = ifname
	}
	iface, err := water.New(config)
	if err != nil {
		panic(err)
	}
	tun.iface = iface
	tun.mtu = getSupportedMTU(mtu, iftapmode)
	// The following check is specific to Linux, as the TAP driver only supports
	// an MTU of 65535-14 to make room for the ethernet headers. This makes sure
	// that the MTU gets rounded down to 65521 instead of causing a panic.
	if iftapmode {
		if tun.mtu > 65535-tun_ETHER_HEADER_LENGTH {
			tun.mtu = 65535 - tun_ETHER_HEADER_LENGTH
		}
	}
	// Friendly output
	tun.log.Infof("Interface name: %s", tun.iface.Name())
	tun.log.Infof("Interface IPv6: %s", addr)
	tun.log.Infof("Interface MTU: %d", tun.mtu)
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
	nlintf, err := netlink.LinkByName(tun.iface.Name())
	if err != nil {
		return err
	}
	if err := netlink.AddrAdd(nlintf, nladdr); err != nil {
		return err
	}
	if err := netlink.LinkSetMTU(nlintf, tun.mtu); err != nil {
		return err
	}
	if err := netlink.LinkSetUp(nlintf); err != nil {
		return err
	}
	return nil
}
