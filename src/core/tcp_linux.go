//go:build linux
// +build linux

package core

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// WARNING: This context is used both by net.Dialer and net.Listen in tcp.go

func (t *tcp) tcpContext(network, address string, c syscall.RawConn) error {
	var control error
	var bbr error

	control = c.Control(func(fd uintptr) {
		bbr = unix.SetsockoptString(int(fd), unix.IPPROTO_TCP, unix.TCP_CONGESTION, "bbr")
	})

	// Log any errors
	if bbr != nil {
		t.links.core.log.Debugln("Failed to set tcp_congestion_control to bbr for socket, SetsockoptString error:", bbr)
	}
	if control != nil {
		t.links.core.log.Debugln("Failed to set tcp_congestion_control to bbr for socket, Control error:", control)
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
		_ = c.Control(btd)
		if err != nil {
			t.links.core.log.Debugln("Failed to set SO_BINDTODEVICE:", sintf)
		}
		return t.tcpContext(network, address, c)
	}
}
