package yggdrasil

// This manages the tun driver to send/recv packets to/from applications

import water "github.com/songgao/water"

const IPv6_HEADER_LENGTH = 40

type tunDevice struct {
	core  *Core
	send  chan<- []byte
	recv  <-chan []byte
	mtu   int
	iface *water.Interface
}

func (tun *tunDevice) init(core *Core) {
	tun.core = core
}

func (tun *tunDevice) write() error {
	for {
		data := <-tun.recv
		if _, err := tun.iface.Write(data); err != nil {
			return err
		}
		util_putBytes(data)
	}
}

func (tun *tunDevice) read() error {
	buf := make([]byte, tun.mtu)
	for {
		n, err := tun.iface.Read(buf)
		if err != nil {
			return err
		}
		if buf[0]&0xf0 != 0x60 ||
			n != 256*int(buf[4])+int(buf[5])+IPv6_HEADER_LENGTH {
			// Either not an IPv6 packet or not the complete packet for some reason
			//panic("Should not happen in testing")
			continue
		}
		packet := append(util_getBytes(), buf[:n]...)
		tun.send <- packet
	}
}

func (tun *tunDevice) close() error {
	return tun.iface.Close()
}
