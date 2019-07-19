package tuntap

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	water "github.com/yggdrasil-network/water"
)

// This is to catch Windows platforms

// Configures the TAP adapter with the correct IPv6 address and MTU. On Windows
// we don't make use of a direct operating system API to do this - we instead
// delegate the hard work to "netsh".
func (tun *TunAdapter) setup(ifname string, iftapmode bool, addr string, mtu int) error {
	if !iftapmode {
		tun.log.Warnln("TUN mode is not supported on this platform, defaulting to TAP")
	}
	config := water.Config{DeviceType: water.TAP}
	config.PlatformSpecificParams.ComponentID = "tap0901"
	config.PlatformSpecificParams.Network = "169.254.0.1/32"
	if ifname == "auto" {
		config.PlatformSpecificParams.InterfaceName = ""
	} else {
		config.PlatformSpecificParams.InterfaceName = ifname
	}
	iface, err := water.New(config)
	if err != nil {
		return err
	}
	if iface.Name() == "" {
		return errors.New("unable to find TAP adapter with component ID " + config.PlatformSpecificParams.ComponentID)
	}
	// Reset the adapter - this invalidates iface so we'll need to get a new one
	if err := tun.resetAdapter(); err != nil {
		return err
	}
	// Get a new iface
	iface, err = water.New(config)
	if err != nil {
		panic(err)
	}
	tun.iface = iface
	tun.mtu = getSupportedMTU(mtu)
	err = tun.setupMTU(tun.mtu)
	if err != nil {
		panic(err)
	}
	// Friendly output
	tun.log.Infof("Interface name: %s", tun.iface.Name())
	tun.log.Infof("Interface IPv6: %s", addr)
	tun.log.Infof("Interface MTU: %d", tun.mtu)
	return tun.setupAddress(addr)
}

// Disable/enable the interface to reset its configuration (invalidating iface).
func (tun *TunAdapter) resetAdapter() error {
	// Bring down the interface first
	cmd := exec.Command("netsh", "interface", "set", "interface", tun.iface.Name(), "admin=DISABLED")
	tun.log.Debugln("netsh command:", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		tun.log.Errorln("Windows netsh failed:", err)
		tun.log.Traceln(string(output))
		return err
	}
	// Bring the interface back up
	cmd = exec.Command("netsh", "interface", "set", "interface", tun.iface.Name(), "admin=ENABLED")
	tun.log.Debugln("netsh command:", strings.Join(cmd.Args, " "))
	output, err = cmd.CombinedOutput()
	if err != nil {
		tun.log.Errorln("Windows netsh failed:", err)
		tun.log.Traceln(string(output))
		return err
	}
	return nil
}

// Sets the MTU of the TAP adapter.
func (tun *TunAdapter) setupMTU(mtu int) error {
	// Set MTU
	cmd := exec.Command("netsh", "interface", "ipv6", "set", "subinterface",
		fmt.Sprintf("interface=%s", tun.iface.Name()),
		fmt.Sprintf("mtu=%d", mtu),
		"store=active")
	tun.log.Debugln("netsh command:", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		tun.log.Errorln("Windows netsh failed:", err)
		tun.log.Traceln(string(output))
		return err
	}
	return nil
}

// Sets the IPv6 address of the TAP adapter.
func (tun *TunAdapter) setupAddress(addr string) error {
	// Set address
	cmd := exec.Command("netsh", "interface", "ipv6", "add", "address",
		fmt.Sprintf("interface=%s", tun.iface.Name()),
		fmt.Sprintf("addr=%s", addr),
		"store=active")
	tun.log.Debugln("netsh command:", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		tun.log.Errorln("Windows netsh failed:", err)
		tun.log.Traceln(string(output))
		return err
	}
	return nil
}
