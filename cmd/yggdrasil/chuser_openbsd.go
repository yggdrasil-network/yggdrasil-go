//go:build openbsd
// +build openbsd

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

	if usr, err = user.Lookup(givenUser); err != nil {
		if usr, err = user.LookupId(givenUser); err != nil {
			return err
		}
	}
	if uid, err = strconv.Atoi(usr.Uid); err != nil {
		return err
	}

	if givenGroup != "" {
		if grp, err = user.LookupGroup(givenGroup); err != nil {
			if grp, err = user.LookupGroupId(givenGroup); err != nil {
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
	if err := unix.Setresgid(gid, gid, gid); err != nil {
		return fmt.Errorf("setresgid: %d: %v", gid, err)
	}
	if err := unix.Setresuid(uid, uid, uid); err != nil {
		return fmt.Errorf("setresuid: %d: %v", uid, err)
	}

	return nil
}
