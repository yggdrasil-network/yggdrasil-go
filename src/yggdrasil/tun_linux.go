package yggdrasil

// The linux platform specific tun parts

import (
	"errors"
	"fmt"
	"net"

	"github.com/docker/libcontainer/netlink"

	water "github.com/yggdrasil-network/water"
)

// Sane defaults for the Linux platform. The "default" options may be
// may be replaced by the running configuration.
func getDefaults() tunDefaultParameters {
	return tunDefaultParameters{
		maximumIfMTU:     65535,
		defaultIfMTU:     65535,
		defaultIfName:    "auto",
		defaultIfTAPMode: false,
	}
}

// Configures the TAP adapter with the correct IPv6 address and MTU.
func (tun *tunDevice) setup(ifname string, iftapmode bool, addr string, mtu int) error {
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
	tun.mtu = getSupportedMTU(mtu)
	return tun.setupAddress(addr)
}

// Configures the TAP adapter with the correct IPv6 address and MTU. Netlink
// is used to do this, so there is not a hard requirement on "ip" or "ifconfig"
// to exist on the system, but this will fail if Netlink is not present in the
// kernel (it nearly always is).
func (tun *tunDevice) setupAddress(addr string) error {
	// Set address
	var netIF *net.Interface
	ifces, err := net.Interfaces()
	if err != nil {
		return err
	}
	for _, ifce := range ifces {
		if ifce.Name == tun.iface.Name() {
			var newIF = ifce
			netIF = &newIF // Don't point inside ifces, it's apparently unsafe?...
		}
	}
	if netIF == nil {
		return errors.New(fmt.Sprintf("Failed to find interface: %s", tun.iface.Name()))
	}
	ip, ipNet, err := net.ParseCIDR(addr)
	if err != nil {
		return err
	}
	err = netlink.NetworkLinkAddIp(netIF, ip, ipNet)
	if err != nil {
		return err
	}
	err = netlink.NetworkSetMTU(netIF, tun.mtu)
	if err != nil {
		return err
	}
	netlink.NetworkLinkUp(netIF)
	if err != nil {
		return err
	}
	return nil
}
