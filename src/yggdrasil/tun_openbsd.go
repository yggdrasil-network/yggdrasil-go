// +build openbsd

package yggdrasil

import "os/exec"
import "unsafe"
import "syscall"
import "golang.org/x/sys/unix"

import water "github.com/neilalexander/water"

// This is to catch OpenBSD

// TODO: Fix TUN mode for OpenBSD. It turns out that OpenBSD doesn't have a way
// to disable the PI header when in TUN mode, so we need to modify the read/
// writes to handle those first four bytes

func getDefaults() tunDefaultParameters {
	return tunDefaultParameters{
		maximumIfMTU:     16384,
		defaultIfMTU:     16384,
		defaultIfName:    "/dev/tap0",
		defaultIfTAPMode: true,
	}
}

// Warning! When porting this to other BSDs, the tuninfo struct can appear with
// the fields in a different order, and the consts below might also have
// different values

type tuninfo struct {
	tun_mtu   uint16
	tun_type  uint32
	tun_flags uint32
	tun_dummy uint16
}

const TUNSIFINFO = (0x80000000) | ((12 & 0x1fff) << 16) | uint32(byte('t'))<<8 | 91
const TUNGIFINFO = (0x40000000) | ((12 & 0x1fff) << 16) | uint32(byte('t'))<<8 | 92
const SIOCAIFADDR_IN6 = (0x80000000) | ((4 & 0x1fff) << 16) | uint32(byte('i'))<<8 | 27

// Below this point seems to be fairly standard at least...

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

func (tun *tunDevice) setup(ifname string, iftapmode bool, addr string, mtu int) error {
	var config water.Config
	if ifname[:4] == "auto" {
		ifname = "/dev/tap0"
	}
	if len(ifname) < 9 {
		panic("TUN/TAP name must be in format /dev/tunX or /dev/tapX")
	}
	switch {
	case iftapmode || ifname[:8] == "/dev/tap":
		config = water.Config{DeviceType: water.TAP}
	case !iftapmode || ifname[:8] == "/dev/tun":
		// config = water.Config{DeviceType: water.TUN}
		panic("TUN mode is not currently supported on OpenBSD, please use TAP instead")
	default:
		panic("TUN/TAP name must be in format /dev/tunX or /dev/tapX")
	}
	config.Name = ifname
	iface, err := water.New(config)
	if err != nil {
		panic(err)
	}
	tun.iface = iface
	tun.mtu = getSupportedMTU(mtu)
	return tun.setupAddress(addr)
}

func (tun *tunDevice) setupAddress(addr string) error {
	fd := tun.iface.FD().Fd()
	var err error
	var ti tuninfo

	tun.core.log.Printf("Interface name: %s", tun.iface.Name())
	tun.core.log.Printf("Interface IPv6: %s", addr)
	tun.core.log.Printf("Interface MTU: %d", tun.mtu)

	// Get the existing interface flags
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(TUNGIFINFO), uintptr(unsafe.Pointer(&ti))); errno != 0 {
		err = errno
		tun.core.log.Printf("Error in TUNGIFINFO: %v", errno)
		return err
	}
	//tun.core.log.Printf("TUNGIFINFO: %+v", ti)

	// Set the new MTU
	ti.tun_mtu = uint16(tun.mtu)

	// Set the new interface flags
	ti.tun_flags |= syscall.IFF_UP
	switch {
	case tun.iface.IsTAP():
		ti.tun_flags |= syscall.IFF_MULTICAST
		ti.tun_flags |= syscall.IFF_BROADCAST
	case tun.iface.IsTUN():
		ti.tun_flags |= syscall.IFF_POINTOPOINT
	}

	// Set the new interface flags
	//tun.core.log.Printf("TUNSIFINFO: %+v", ti)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(TUNSIFINFO), uintptr(unsafe.Pointer(&ti))); errno != 0 {
		err = errno
		tun.core.log.Printf("Error in TUNSIFINFO: %v", errno)
		return err
	}

	// Set address
	cmd := exec.Command("ifconfig", tun.iface.Name(), "inet6", addr)
	//tun.core.log.Printf("ifconfig command: %v", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		tun.core.log.Printf("ifconfig failed: %v.", err)
		tun.core.log.Println(string(output))
	}

	return nil
}
