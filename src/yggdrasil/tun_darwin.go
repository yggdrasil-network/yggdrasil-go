package yggdrasil

// The darwin platform specific tun parts
// macOS/Darwin is BSD-derived and doesn't have iproute2. This code instead
// uses ifconfig. There is actually code that tries to do this properly using
// syscalls in github.com/neilalexander/yggdrasil-go branch "macos-interface"
// but for some reason it doesn't work as expected!

import "fmt"
import "os/exec"
import "strings"

import water "github.com/songgao/water"

func (tun *tunDevice) setup(ifname string, addr string, mtu int) error {
	config := water.Config{DeviceType: water.TUN}
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
	cmd := exec.Command("ifconfig", tun.iface.Name(), "inet6",
		"add", addr)
	tun.core.log.Printf("ifconfig command: %v", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		tun.core.log.Printf("Darwin ifconfig failed: %v.", err)
		tun.core.log.Println(string(output))
		return err
	}
	// Set MTU and bring device up
	cmd = exec.Command("ifconfig", tun.iface.Name(), "mtu",
		fmt.Sprintf("%d", tun.mtu))
	tun.core.log.Printf("ifconfig command: %v", strings.Join(cmd.Args, " "))
	output, err = cmd.CombinedOutput()
	if err != nil {
		tun.core.log.Printf("Darwin ifconfig failed: %v.", err)
		tun.core.log.Println(string(output))
		return err
	}
	return nil
}
