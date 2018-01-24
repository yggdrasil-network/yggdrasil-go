package yggdrasil

import water "github.com/songgao/water"
import "os/exec"
import "strings"
import "fmt"

// This is to catch Windows platforms

func (tun *tunDevice) setup(ifname string, addr string, mtu int) error {
	config := water.Config{DeviceType: water.TAP}
	config.PlatformSpecificParams.ComponentID = "tap0901"
	config.PlatformSpecificParams.Network = "169.254.0.1/32"
	iface, err := water.New(config)
	if err != nil {
		panic(err)
	}
	tun.iface = iface
	tun.mtu = mtu
	return tun.setupAddress(addr)
}

func (tun *tunDevice) setupAddress(addr string) error {
	// Set address
	// addr = strings.TrimRight(addr, "/8")
	cmd := exec.Command("netsh", "interface", "ipv6", "set", "address",
		fmt.Sprintf("interface=\"%s\"", tun.iface.Name()),
		fmt.Sprintf("addr=\"%s\"", addr))
	tun.core.log.Printf("netsh command: %v", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		tun.core.log.Printf("Windows netsh failed: %v.", err)
		tun.core.log.Println(string(output))
		return err
	}
	return nil
}
