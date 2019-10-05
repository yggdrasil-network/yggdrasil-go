// +build linux

package yggdrasil

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// WARNING: This context is used both by net.Dialer and net.Listen in tcp.go

func (t *tcp) tcpContext(network, address string, c syscall.RawConn) error {
	var control error
	var bbr error

	control = c.Control(func(fd uintptr) {
		// sys/socket.h: #define	SO_RECV_ANYIF	0x1104
		bbr = unix.SetsockoptString(int(fd), unix.IPPROTO_TCP, unix.TCP_CONGESTION, "bbr")
	})

	switch {
	case bbr != nil:
		return bbr
	default:
		return control
	}
}
