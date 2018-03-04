package yggdrasil

import "syscall"

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

/*
OpenBSD, net/if_tun.h:

struct tuninfo {
        u_int   mtu;
        u_short type;
        u_short flags;
        u_int   baudrate;
};
*/

type tuninfo struct {
	tun_mtu      uint32
	tun_type     uint16
	tun_flags    uint16
	tun_baudrate uint32
}

func (ti *tuninfo) setInfo(tun *tunDevice) {
	ti.tun_flags |= syscall.IFF_UP
	switch {
	case tun.iface.IsTAP():
		ti.tun_flags |= syscall.IFF_MULTICAST
		ti.tun_flags |= syscall.IFF_BROADCAST
	case tun.iface.IsTUN():
		ti.tun_flags |= syscall.IFF_POINTOPOINT
	}
  ti.tun_mtu = uint32(tun.mtu)
}

const TUNSIFINFO = (0x80000000) | ((12 & 0x1fff) << 16) | uint32(byte('t'))<<8 | 91
const TUNGIFINFO = (0x40000000) | ((12 & 0x1fff) << 16) | uint32(byte('t'))<<8 | 92
const SIOCAIFADDR_IN6 = (0x80000000) | ((4 & 0x1fff) << 16) | uint32(byte('i'))<<8 | 27

