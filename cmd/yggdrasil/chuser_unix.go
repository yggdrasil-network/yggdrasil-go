//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris
// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris

package main

import (
	"errors"
	"fmt"
	"math"
	osuser "os/user"
	"strconv"
	"strings"
	"syscall"
)

func chuser(user string) error {
	group := ""
	if i := strings.IndexByte(user, ':'); i >= 0 {
		user, group = user[:i], user[i+1:]
	}

	u := (*osuser.User)(nil)
	g := (*osuser.Group)(nil)

	if user != "" {
		if _, err := strconv.ParseUint(user, 10, 32); err == nil {
			u, err = osuser.LookupId(user)
			if err != nil {
				return fmt.Errorf("failed to lookup user by id %q: %v", user, err)
			}
		} else {
			u, err = osuser.Lookup(user)
			if err != nil {
				return fmt.Errorf("failed to lookup user by name %q: %v", user, err)
			}
		}
	}
	if group != "" {
		if _, err := strconv.ParseUint(group, 10, 32); err == nil {
			g, err = osuser.LookupGroupId(group)
			if err != nil {
				return fmt.Errorf("failed to lookup group by id %q: %v", user, err)
			}
		} else {
			g, err = osuser.LookupGroup(group)
			if err != nil {
				return fmt.Errorf("failed to lookup group by name %q: %v", user, err)
			}
		}
	}

	if g != nil {
		gid, _ := strconv.ParseUint(g.Gid, 10, 32)
		var err error
		if gid < math.MaxInt {
			if err := syscall.Setgroups([]int{int(gid)}); err != nil {
				return fmt.Errorf("failed to setgroups %d: %v", gid, err)
			}
			err = syscall.Setgid(int(gid))
		} else {
			err = errors.New("gid too big")
		}

		if err != nil {
			return fmt.Errorf("failed to setgid %d: %v", gid, err)
		}
	} else if u != nil {
		gid, _ := strconv.ParseUint(u.Gid, 10, 32)
		if err := syscall.Setgroups([]int{int(uint32(gid))}); err != nil {
			return fmt.Errorf("failed to setgroups %d: %v", gid, err)
		}
		err := syscall.Setgid(int(uint32(gid)))
		if err != nil {
			return fmt.Errorf("failed to setgid %d: %v", gid, err)
		}
	}

	if u != nil {
		uid, _ := strconv.ParseUint(u.Uid, 10, 32)
		var err error
		if uid < math.MaxInt {
			err = syscall.Setuid(int(uid))
		} else {
			err = errors.New("uid too big")
		}

		if err != nil {
			return fmt.Errorf("failed to setuid %d: %v", uid, err)
		}
	}

	return nil
}
