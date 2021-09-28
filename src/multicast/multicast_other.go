//go:build !linux && !darwin && !ios && !netbsd && !freebsd && !openbsd && !dragonflybsd && !windows
// +build !linux,!darwin,!ios,!netbsd,!freebsd,!openbsd,!dragonflybsd,!windows

package multicast

import "syscall"

func (m *Multicast) _multicastStarted() {

}

func (m *Multicast) multicastReuse(network string, address string, c syscall.RawConn) error {
	return nil
}
