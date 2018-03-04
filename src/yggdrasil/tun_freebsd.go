package yggdrasil

// This is to catch FreeBSD and NetBSD

func getDefaults() tunDefaultParameters {
	return tunDefaultParameters{
		maximumIfMTU:     32767,
		defaultIfMTU:     32767,
		defaultIfName:    "/dev/tap0",
		defaultIfTAPMode: true,
	}
}

// Warning! When porting this to other BSDs, the tuninfo struct can appear with
// the fields in a different order, and the consts below might also have
// different values

/*
FreeBSD/NetBSD, net/if_tun.h:

struct tuninfo {
	int	baudrate;
	short	mtu;
	u_char	type;
	u_char	dummy;
};
*/

type tuninfo struct {
	tun_baudrate int32
	tun_mtu      int16
	tun_type     uint8
	tun_dummy    uint8
}

func (ti *tuninfo) setInfo(tun *tunDevice) {
	ti.tun_mtu = int16(tun.mtu)
}

const TUNSIFINFO = (0x80000000) | ((8 & 0x1fff) << 16) | uint32(byte('t'))<<8 | 91
const TUNGIFINFO = (0x40000000) | ((8 & 0x1fff) << 16) | uint32(byte('t'))<<8 | 92
const TUNSIFHEAD = (0x80000000) | ((4 & 0x1fff) << 16) | uint32(byte('t'))<<8 | 96
const SIOCAIFADDR_IN6 = (0x80000000) | ((4 & 0x1fff) << 16) | uint32(byte('i'))<<8 | 27
