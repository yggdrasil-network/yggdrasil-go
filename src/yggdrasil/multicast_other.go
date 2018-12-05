// +build !linux,!darwin,!netbsd,!freebsd,!openbsd,!dragonflybsd

package yggdrasil

import "syscall"

func multicastReuse(network string, address string, c syscall.RawConn) error {
	return nil
}
