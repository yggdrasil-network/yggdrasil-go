// +build !unix

package main

import "errors"

func setuid(uid int) error {
	return errors.New("setting uid not supported on this platform")
}

func setgid(gid int) error {
	return errors.New("setting gid not supported on this platform")
}
