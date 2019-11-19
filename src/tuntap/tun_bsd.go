// +build openbsd freebsd netbsd

package tuntap

import (
	"encoding/binary"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/yggdrasil-network/water"
)

const SIOCSIFADDR_IN6 = (0x80000000) | ((288 & 0x1fff) << 16) | uint32(byte('i'))<<8 | 12

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

/*
from <netinet6/in6_var.h>
struct  in6_ifreq {
 277         char    ifr_name[IFNAMSIZ];
 278         union {
 279                 struct  sockaddr_in6 ifru_addr;
 280                 struct  sockaddr_in6 ifru_dstaddr;
 281                 int     ifru_flags;
 282                 int     ifru_flags6;
 283                 int     ifru_metric;
 284                 caddr_t ifru_data;
 285                 struct in6_addrlifetime ifru_lifetime;
 286                 struct in6_ifstat ifru_stat;
 287                 struct icmp6_ifstat ifru_icmp6stat;
 288                 u_int32_t ifru_scope_id[16];
 289         } ifr_ifru;
 290 };
*/

type in6_ifreq_mtu struct {
	ifr_name [syscall.IFNAMSIZ]byte
	ifru_mtu int
}

type in6_ifreq_addr struct {
	ifr_name  [syscall.IFNAMSIZ]byte
	ifru_addr sockaddr_in6
}

type in6_ifreq_flags struct {
	ifr_name [syscall.IFNAMSIZ]byte
	flags    int
}

type in6_ifreq_lifetime struct {
	ifr_name          [syscall.IFNAMSIZ]byte
	ifru_addrlifetime in6_addrlifetime
}

// Sets the IPv6 address of the utun adapter. On all BSD platforms (FreeBSD,
// OpenBSD, NetBSD) an attempt is made to set the adapter properties by using
// a system socket and making syscalls to the kernel. This is not refined though
// and often doesn't work (if at all), therefore if a call fails, it resorts
// to calling "ifconfig" instead.
func (tun *TunAdapter) setup(ifname string, iftapmode bool, addr string, mtu int) error {
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
		panic("TUN mode is not currently supported on this platform, please use TAP instead")
	default:
		panic("TUN/TAP name must be in format /dev/tunX or /dev/tapX")
	}
	config.Name = ifname
	iface, err := water.New(config)
	if err != nil {
		panic(err)
	}
	tun.iface = iface
	tun.mtu = getSupportedMTU(mtu, iftapmode)
	return tun.setupAddress(addr)
}

func (tun *TunAdapter) setupAddress(addr string) error {
	var sfd int
	var err error

	// Create system socket
	if sfd, err = unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0); err != nil {
		tun.log.Printf("Create AF_INET socket failed: %v.", err)
		return err
	}

	// Friendly output
	tun.log.Infof("Interface name: %s", tun.iface.Name())
	tun.log.Infof("Interface IPv6: %s", addr)
	tun.log.Infof("Interface MTU: %d", tun.mtu)

	// Create the MTU request
	var ir in6_ifreq_mtu
	copy(ir.ifr_name[:], tun.iface.Name())
	ir.ifru_mtu = int(tun.mtu)

	// Set the MTU
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(sfd), uintptr(syscall.SIOCSIFMTU), uintptr(unsafe.Pointer(&ir))); errno != 0 {
		err = errno
		tun.log.Errorf("Error in SIOCSIFMTU: %v", errno)

		// Fall back to ifconfig to set the MTU
		cmd := exec.Command("ifconfig", tun.iface.Name(), "mtu", string(tun.mtu))
		tun.log.Warnf("Using ifconfig as fallback: %v", strings.Join(cmd.Args, " "))
		output, err := cmd.CombinedOutput()
		if err != nil {
			tun.log.Errorf("SIOCSIFMTU fallback failed: %v.", err)
			tun.log.Traceln(string(output))
		}
	}

	// Create the address request
	// FIXME: I don't work!
	var ar in6_ifreq_addr
	copy(ar.ifr_name[:], tun.iface.Name())
	ar.ifru_addr.sin6_len = uint8(unsafe.Sizeof(ar.ifru_addr))
	ar.ifru_addr.sin6_family = unix.AF_INET6
	parts := strings.Split(strings.Split(addr, "/")[0], ":")
	for i := 0; i < 8; i++ {
		addr, _ := strconv.ParseUint(parts[i], 16, 16)
		b := make([]byte, 16)
		binary.LittleEndian.PutUint16(b, uint16(addr))
		ar.ifru_addr.sin6_addr[i] = uint16(binary.BigEndian.Uint16(b))
	}

	// Set the interface address
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(sfd), uintptr(SIOCSIFADDR_IN6), uintptr(unsafe.Pointer(&ar))); errno != 0 {
		err = errno
		tun.log.Errorf("Error in SIOCSIFADDR_IN6: %v", errno)

		// Fall back to ifconfig to set the address
		cmd := exec.Command("ifconfig", tun.iface.Name(), "inet6", addr)
		tun.log.Warnf("Using ifconfig as fallback: %v", strings.Join(cmd.Args, " "))
		output, err := cmd.CombinedOutput()
		if err != nil {
			tun.log.Errorf("SIOCSIFADDR_IN6 fallback failed: %v.", err)
			tun.log.Traceln(string(output))
		}
	}

	return nil
}
