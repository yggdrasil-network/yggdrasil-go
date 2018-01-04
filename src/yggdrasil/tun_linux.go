package yggdrasil

// The linux platform specific tun parts
// It depends on iproute2 being installed to set things on the tun device

import "fmt"
import "os/exec"
import "strings"

import water "github.com/songgao/water"

func (tun *tunDevice) setup(ifname string, addr string, mtu int) error {
	config := water.Config{DeviceType: water.TUN}
	if ifname != "" && ifname != "auto" {
		config.Name = ifname
	}
	iface, err := water.New(config)
	if err != nil {
		panic(err)
	}
	tun.iface = iface
	tun.mtu = mtu //1280 // Lets default to the smallest thing allowed for now
	return tun.setupAddress(addr)
}

func (tun *tunDevice) setupAddress(addr string) error {
	// Set address
	cmd := exec.Command("ip", "-f", "inet6",
		"addr", "add", addr,
		"dev", tun.iface.Name())
	tun.core.log.Printf("ip command: %v", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		tun.core.log.Printf("Linux ip failed: %v.", err)
		tun.core.log.Println(string(output))
		return err
	}
	// Set MTU and bring device up
	cmd = exec.Command("ip", "link", "set",
		"dev", tun.iface.Name(),
		"mtu", fmt.Sprintf("%d", tun.mtu),
		"up")
	tun.core.log.Printf("ip command: %v", strings.Join(cmd.Args, " "))
	output, err = cmd.CombinedOutput()
	if err != nil {
		tun.core.log.Printf("Linux ip failed: %v.", err)
		tun.core.log.Println(string(output))
		return err
	}
	return nil
}
