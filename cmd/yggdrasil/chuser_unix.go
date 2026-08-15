//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func chuser(input string, adminSock net.Addr) error {
	givenUser, givenGroup, _ := strings.Cut(input, ":")
	if givenUser == "" {
		return fmt.Errorf("user is empty")
	}
	if strings.Contains(input, ":") && givenGroup == "" {
		return fmt.Errorf("group is empty")
	}

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

	// Ask the admin socket where it actually ended up rather than parsing the
	// configured address a second time, since admin.New has its own rules for
	// interpreting it and the two parsers can only disagree.
	if adminSock != nil && adminSock.Network() == "unix" {
		// Abstract sockets exist in the kernel namespace rather than on the
		// filesystem, so there is nothing to change the ownership of.
		if path := adminSock.String(); path != "" && path[0] != '@' {
			if err := os.Chown(path, uid, gid); err != nil {
				return fmt.Errorf("chown %s %d:%d: %v", path, uid, gid, err)
			}
		}
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
