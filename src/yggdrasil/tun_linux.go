package yggdrasil

// The linux platform specific tun parts
// It depends on iproute2 being installed to set things on the tun device

import "errors"
import "fmt"
import "net"

import water "github.com/neilalexander/water"

import "github.com/docker/libcontainer/netlink"

func getDefaults() tunDefaultParameters {
	return tunDefaultParameters{
		maximumIfMTU:     65535,
		defaultIfMTU:     65535,
		defaultIfName:    "auto",
		defaultIfTAPMode: false,
	}
}

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

func (tun *tunDevice) setupAddress(addr string) error {
	// Set address
	var netIF *net.Interface
	ifces, err := net.Interfaces()
	if err != nil {
		return err
	}
	for _, ifce := range ifces {
		if ifce.Name == tun.iface.Name() {
			netIF = &ifce
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
