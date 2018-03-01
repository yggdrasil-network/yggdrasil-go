// +build openbsd freebsd solaris netbsd dragonfly

package yggdrasil

import "fmt"
import "os/exec"
import "strings"

import water "github.com/neilalexander/water"

// This is to catch BSD platforms

const TUNSIFINFO = (0x80000000) | ((4 & 0x1fff) << 16) | uint32(byte('t'))<<8 | 91
const TUNGIFINFO = (0x40000000) | ((4 & 0x1fff) << 16) | uint32(byte('t'))<<8 | 92
const SIOCAIFADDR_IN6 = (0x80000000) | ((4 & 0x1fff) << 16) | uint32(byte('i'))<<8 | 27

type in6_addrlifetime struct {
	ia6t_expire    float64
	ia6t_preferred float64
	ia6t_vltime    uint32
	ia6t_pltime    uint32
}

type sockaddr_in6 struct {
	sin6_len      uint8
	sin6_family   uint8
	sin6_port     uint8
	sin6_flowinfo uint32
	sin6_addr     [8]uint16
	sin6_scope_id uint32
}

type in6_aliasreq struct {
	ifra_name       [16]byte
	ifra_addr       sockaddr_in6
	ifra_dstaddr    sockaddr_in6
	ifra_prefixmask sockaddr_in6
	ifra_flags      uint32
	ifra_lifetime   in6_addrlifetime
}

type tuninfo struct {
	tun_baudrate int
	tun_mtu      int16
	tun_type     uint8
	tun_dummy    uint8
}

func (tun *tunDevice) setup(ifname string, iftapmode bool, addr string, mtu int) error {
	var config water.Config
	switch {
	case iftapmode || ifname[:8] == "/dev/tap":
		config = water.Config{DeviceType: water.TAP}
	case !iftapmode || ifname[:8] == "/dev/tun":
		config = water.Config{DeviceType: water.TUN}
	default:
		panic("TUN/TAP name must be in format /dev/tunX or /dev/tapX")
	}
	config.Name = ifname
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
	cmd := exec.Command("ifconfig", tun.iface.Name(), "inet6", addr)
	tun.core.log.Printf("ifconfig command: %v", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		tun.core.log.Printf("ipconfig failed: %v.", err)
		tun.core.log.Println(string(output))
		return err
	}
	// Set MTU and bring device up
	cmd = exec.Command("ifconfig", tun.iface.Name(), "mtu", fmt.Sprintf("%d", tun.mtu), "up")
	tun.core.log.Printf("ifconfig command: %v", strings.Join(cmd.Args, " "))
	output, err = cmd.CombinedOutput()
	if err != nil {
		tun.core.log.Printf("ipconfig failed: %v.", err)
		tun.core.log.Println(string(output))
		return err
	}

	return nil
}
