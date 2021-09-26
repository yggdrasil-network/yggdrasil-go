//go:build darwin
// +build darwin

package core

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// WARNING: This context is used both by net.Dialer and net.Listen in tcp.go

func (t *tcp) tcpContext(network, address string, c syscall.RawConn) error {
	var control error
	var recvanyif error

	control = c.Control(func(fd uintptr) {
		// sys/socket.h: #define	SO_RECV_ANYIF	0x1104
		recvanyif = unix.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 0x1104, 1)
	})

	switch {
	case recvanyif != nil:
		return recvanyif
	default:
		return control
	}
}

func (t *tcp) getControl(sintf string) func(string, string, syscall.RawConn) error {
	return t.tcpContext
}
