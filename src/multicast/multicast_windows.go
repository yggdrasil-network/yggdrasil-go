//go:build windows
// +build windows

package multicast

import (
	"syscall"

	"golang.org/x/sys/windows"
)

func (m *Multicast) _multicastStarted() {

}

func (m *Multicast) multicastReuse(network string, address string, c syscall.RawConn) error {
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
