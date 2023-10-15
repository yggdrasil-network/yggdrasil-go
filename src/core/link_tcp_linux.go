//go:build linux
// +build linux

package core

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// WARNING: This context is used both by net.Dialer and net.Listen in tcp.go

func (t *linkTCP) tcpContext(network, address string, c syscall.RawConn) error {
	return nil
}

func (t *linkTCP) getControl(sintf string) func(string, string, syscall.RawConn) error {
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
