// +build linux netbsd freebsd openbsd dragonflybsd

package yggdrasil

import "syscall"
import "golang.org/x/sys/unix"

func (m *multicast) multicastWake() {

}

func (m *multicast) multicastReuse(network string, address string, c syscall.RawConn) error {
	var control error
	var reuseport error

	control = c.Control(func(fd uintptr) {
		reuseport = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	})

	switch {
	case reuseport != nil:
		return reuseport
	default:
		return control
	}
}
