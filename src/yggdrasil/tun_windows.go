package yggdrasil

import (
	"fmt"
	"os/exec"
	"strings"

	water "github.com/yggdrasil-network/water"
)

// This is to catch Windows platforms

// Sane defaults for the Windows platform. The "default" options may be
// may be replaced by the running configuration.
func getDefaults() tunDefaultParameters {
	return tunDefaultParameters{
		maximumIfMTU:     65535,
		defaultIfMTU:     65535,
		defaultIfName:    "auto",
		defaultIfTAPMode: true,
	}
}

// Configures the TAP adapter with the correct IPv6 address and MTU. On Windows
// we don't make use of a direct operating system API to do this - we instead
// delegate the hard work to "netsh".
func (tun *tunDevice) setup(ifname string, iftapmode bool, addr string, mtu int) error {
	if !iftapmode {
		tun.core.log.Printf("TUN mode is not supported on this platform, defaulting to TAP")
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
		panic(err)
	}
	// Disable/enable the interface to resets its configuration (invalidating iface)
	cmd := exec.Command("netsh", "interface", "set", "interface", iface.Name(), "admin=DISABLED")
	tun.core.log.Printf("netsh command: %v", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		tun.core.log.Printf("Windows netsh failed: %v.", err)
		tun.core.log.Println(string(output))
		return err
	}
	cmd = exec.Command("netsh", "interface", "set", "interface", iface.Name(), "admin=ENABLED")
	tun.core.log.Printf("netsh command: %v", strings.Join(cmd.Args, " "))
	output, err = cmd.CombinedOutput()
	if err != nil {
		tun.core.log.Printf("Windows netsh failed: %v.", err)
		tun.core.log.Println(string(output))
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
	return tun.setupAddress(addr)
}

// Sets the MTU of the TAP adapter.
func (tun *tunDevice) setupMTU(mtu int) error {
	// Set MTU
	cmd := exec.Command("netsh", "interface", "ipv6", "set", "subinterface",
		fmt.Sprintf("interface=%s", tun.iface.Name()),
		fmt.Sprintf("mtu=%d", mtu),
		"store=active")
	tun.core.log.Printf("netsh command: %v", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		tun.core.log.Printf("Windows netsh failed: %v.", err)
		tun.core.log.Println(string(output))
		return err
	}
	return nil
}

// Sets the IPv6 address of the TAP adapter.
func (tun *tunDevice) setupAddress(addr string) error {
	// Set address
	cmd := exec.Command("netsh", "interface", "ipv6", "add", "address",
		fmt.Sprintf("interface=%s", tun.iface.Name()),
		fmt.Sprintf("addr=%s", addr),
		"store=active")
	tun.core.log.Printf("netsh command: %v", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		tun.core.log.Printf("Windows netsh failed: %v.", err)
		tun.core.log.Println(string(output))
		return err
	}
	return nil
}
