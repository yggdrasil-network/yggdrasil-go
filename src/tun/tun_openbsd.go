//go:build openbsd
// +build openbsd

package tun

import (
	"fmt"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

const SIOCAIFADDR_IN6 = 0x8080691a

type in6_addrlifetime struct {
	ia6t_expire    int64
	ia6t_preferred int64
	ia6t_vltime    uint32
	ia6t_pltime    uint32
}

// Match types from the net package, effectively being [16]byte for IPv6 addresses.
type in6_addr [16]uint8

type sockaddr_in6 struct {
	sin6_len      uint8
	sin6_family   uint8
	sin6_port     uint16
	sin6_flowinfo uint32
	sin6_addr     in6_addr
	sin6_scope_id uint32
}

type in6_aliasreq struct {
	ifra_name       [syscall.IFNAMSIZ]byte
	ifra_addr       sockaddr_in6
	ifra_dstaddr    sockaddr_in6
	ifra_prefixmask sockaddr_in6
	ifra_flags      int32
	ifra_lifetime   in6_addrlifetime
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

	ip, _, err := net.ParseCIDR(addr)
	if err != nil {
		tun.log.Errorf("Error in ParseCIDR: %v", err)
		return err
	}

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
	var ar in6_aliasreq
	copy(ar.ifra_name[:], tun.Name())
	ar.ifra_addr.sin6_len = uint8(unsafe.Sizeof(ar.ifra_addr))
	ar.ifra_addr.sin6_family = unix.AF_INET6

	for i := range in6_aliasreq.sin6_addr {
		sa6.sin6_addr[i] = addr[i]
	}

	// Set the interface address
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(sfd), uintptr(SIOCAIFADDR_IN6), uintptr(unsafe.Pointer(&ar))); errno != 0 {
		err = errno
		tun.log.Errorf("Error in SIOCAIFADDR_IN6: %v", errno)
	}

	return nil
}
