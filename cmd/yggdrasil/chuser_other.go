//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package main

import (
	"errors"
	"net"
)

func chuser(user string, adminSock net.Addr) error {
	return errors.New("setting uid/gid is not supported on this platform")
}
