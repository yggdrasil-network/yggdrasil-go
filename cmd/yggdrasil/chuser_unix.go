//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris
// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris

package main

import (
	"fmt"
	"os/user"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func chuser(input string) error {
	givenUser, givenGroup, _ := strings.Cut(input, ":")

	var (
		err      error
		usr      *user.User
		grp      *user.Group
		uid, gid int
	)

	if usr, err = user.LookupId(givenUser); err != nil {
		if usr, err = user.Lookup(givenUser); err != nil {
			return err
		}
	}
	if uid, err = strconv.Atoi(usr.Uid); err != nil {
		return err
	}

	if givenGroup != "" {
		if grp, err = user.LookupGroupId(givenGroup); err != nil {
			if grp, err = user.LookupGroup(givenGroup); err != nil {
				return err
			}
		}

		gid, _ = strconv.Atoi(grp.Gid)
	} else {
		gid, _ = strconv.Atoi(usr.Gid)
	}

	if err := unix.Setgroups([]int{gid}); err != nil {
		return fmt.Errorf("setgroups: %d: %v", gid, err)
	}
	if err := unix.Setgid(gid); err != nil {
		return fmt.Errorf("setgid: %d: %v", gid, err)
	}
	if err := unix.Setuid(uid); err != nil {
		return fmt.Errorf("setuid: %d: %v", uid, err)
	}

	return nil
}
