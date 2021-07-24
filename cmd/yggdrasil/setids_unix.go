// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris

package main

import "syscall"

func setuid(uid int) error {
	return syscall.Setuid(uid)
}

func setgid(gid int) error {
	return syscall.Setgid(gid)
}
