//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package main

import "errors"

func chuser(user, adminSockUrl string) error {
	return errors.New("setting uid/gid is not supported on this platform")
}
