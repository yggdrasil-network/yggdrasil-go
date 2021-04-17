// +build linux

package yggdrasil

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// WARNING: This context is used both by net.Dialer and net.Listen in tcp.go

func (t *tcp) tcpContext(network, address string, c syscall.RawConn) error {
	var tcp_cca = t.links.core.config.Current.TCPCongestionControl
	var control error
	var ret error

	// do not change TCP congestion control algorithm so that a system-wide one is used
	if tcp_cca == "" {
		return nil
	}

	control = c.Control(func(fd uintptr) {
		ret = unix.SetsockoptString(int(fd), unix.IPPROTO_TCP, unix.TCP_CONGESTION, tcp_cca)
	})

	// Log any errors
	if ret != nil {
		t.links.core.log.Debugf("Failed to set tcp_congestion_control to %s for socket, SetsockoptString error: %s\n", tcp_cca, ret)
	}
	if control != nil {
		t.links.core.log.Debugf("Failed to set tcp_congestion_control to %s for socket, Control error:\n", tcp_cca, control)
	}

	// Return nil because errors here are not considered fatal for the connection, it just means congestion control is suboptimal
	return nil
}

func (t *tcp) getControl(sintf string) func(string, string, syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var err error
		btd := func(fd uintptr) {
			err = unix.BindToDevice(int(fd), sintf)
		}
		c.Control(btd)
		if err != nil {
			t.links.core.log.Debugln("Failed to set SO_BINDTODEVICE:", sintf)
		}
		return t.tcpContext(network, address, c)
	}
}
