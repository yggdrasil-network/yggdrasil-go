package tuntap

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

// This is to catch Windows platforms

// Configures the TUN adapter with the correct IPv6 address and MTU.
func (tun *TunAdapter) setup(ifname string, addr string, mtu int) error {
	iface, err := wgtun.CreateTUN(ifname, mtu)
	if err != nil {
		panic(err)
	}
	tun.iface = iface
	if mtu, err := iface.MTU(); err == nil {
		tun.mtu = getSupportedMTU(mtu)
	} else {
		tun.mtu = 0
	}
	return tun.setupAddress(addr)
}

// Sets the MTU of the TAP adapter.
func (tun *TunAdapter) setupMTU(mtu int) error {
	if tun.iface == nil || tun.iface.Name() == "" {
		return errors.New("Can't configure MTU as TAP adapter is not present")
	}
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
	time.Sleep(time.Second) // FIXME artifical delay to give netsh time to take effect
	return nil
}

// Sets the IPv6 address of the TAP adapter.
func (tun *TunAdapter) setupAddress(addr string) error {
	if tun.iface == nil || tun.iface.Name() == "" {
		return errors.New("Can't configure IPv6 address as TAP adapter is not present")
	}
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
	time.Sleep(time.Second) // FIXME artifical delay to give netsh time to take effect
	return nil
}
