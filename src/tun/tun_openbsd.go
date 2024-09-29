//go:build openbsd
// +build openbsd

package tun

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

const SIOCSIFADDR_IN6 = (0x80000000) | ((288 & 0x1fff) << 16) | uint32(byte('i'))<<8 | 12

type in6_addrlifetime struct {
	ia6t_expire    int64
	ia6t_preferred int64
	ia6t_vltime    uint32
	ia6t_pltime    uint32
}

type sockaddr_in6 struct {
	sin6_len      uint8
	sin6_family   uint8
	sin6_port     uint16
	sin6_flowinfo uint32
	sin6_addr     [8]uint16
	sin6_scope_id uint32
}

type in6_ifreq_addr struct {
	ifr_name  [syscall.IFNAMSIZ]byte
	ifru_addr sockaddr_in6
}

// Configures the TUN adapter with the correct IPv6 address and MTU.
func (tun *TunAdapter) setup(ifname string, addr string, mtu uint64) error {
	iface, err := wgtun.CreateTUN(ifname, int(mtu))
	if err != nil {
		return fmt.Errorf("failed to create TUN: %w", err)
	}
	tun.iface = iface
	if mtu, err := iface.MTU(); err == nil {
		tun.mtu = getSupportedMTU(uint64(mtu))
	} else {
		tun.mtu = 0
	}
	if addr != "" {
		return tun.setupAddress(addr)
	}
	return nil
}

// Configures the "utun" adapter from an existing file descriptor.
func (tun *TunAdapter) setupFD(fd int32, addr string, mtu uint64) error {
	return fmt.Errorf("setup via FD not supported on this platform")
}

func (tun *TunAdapter) setupAddress(addr string) error {
	var sfd int
	var err error

	// Create system socket
	if sfd, err = unix.Socket(unix.AF_INET6, unix.SOCK_DGRAM, 0); err != nil {
		tun.log.Printf("Create AF_INET6 socket failed: %v", err)
		return err
	}

	// Friendly output
	tun.log.Infof("Interface name: %s", tun.Name())
	tun.log.Infof("Interface IPv6: %s", addr)
	tun.log.Infof("Interface MTU: %d", tun.mtu)

	// Create the address request
	// FIXME: I don't work!
	var ar in6_ifreq_addr
	copy(ar.ifr_name[:], tun.Name())
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
	}

	return nil
}
