// +build windows

package yggdrasil

import "syscall"
import "golang.org/x/sys/windows"

func (m *multicast) multicastWake() {

}

func (m *multicast) multicastReuse(network string, address string, c syscall.RawConn) error {
	var control error
	var reuseaddr error

	control = c.Control(func(fd uintptr) {
		reuseaddr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
	})

	switch {
	case reuseaddr != nil:
		return reuseaddr
	default:
		return control
	}
}
