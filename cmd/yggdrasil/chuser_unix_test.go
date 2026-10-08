//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"net"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// Usernames must not contain a number sign.
func TestEmptyString(t *testing.T) {
	if chuser("", nil) == nil {
		t.Fatal("the empty string is not a valid user")
	}
}

// Either omit delimiter and group, or omit both.
func TestEmptyGroup(t *testing.T) {
	if chuser("0:", nil) == nil {
		t.Fatal("the empty group is not allowed")
	}
}

// Either user only or user and group.
func TestGroupOnly(t *testing.T) {
	if chuser(":0", nil) == nil {
		t.Fatal("group only is not allowed")
	}
}

// Usenames must not contain the number sign.
func TestInvalidUsername(t *testing.T) {
	const username = "#user"
	if chuser(username, nil) == nil {
		t.Fatalf("'%s' is not a valid username", username)
	}
}

// User IDs must be non-negative.
func TestInvalidUserid(t *testing.T) {
	if chuser("-1", nil) == nil {
		t.Fatal("User ID cannot be negative")
	}
}

// isChownError reports whether the admin socket ownership step is what failed.
// Everything after it needs privileges that the test process may not have, so
// the remaining errors are not interesting here.
func isChownError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "chown ")
}

// Admin socket addresses that aren't a UNIX socket on disk must be left alone
// rather than treated as a failure to hand the socket over.
func TestAdminSocketWithNothingToChown(t *testing.T) {
	usr, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	for _, addr := range []net.Addr{
		nil, // admin socket disabled
		&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9001},
		&net.UnixAddr{Net: "unix", Name: "@yggdrasil"}, // abstract, no filesystem entry
	} {
		if err := chuser(usr.Uid, addr); isChownError(err) {
			t.Errorf("admin socket %v should not have been chowned: %v", addr, err)
		}
	}
}

// A UNIX admin socket that exists must have its ownership changed.
func TestAdminSocketOwnership(t *testing.T) {
	usr, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "admin.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("cannot create a UNIX socket: %v", err)
	}
	defer l.Close()

	if err := chuser(usr.Uid, l.Addr()); isChownError(err) {
		t.Fatalf("failed to chown the admin socket: %v", err)
	}
}

// A UNIX admin socket that cannot be chowned must stop us from dropping
// privileges, otherwise the target user is left without a usable socket.
func TestAdminSocketOwnershipFailure(t *testing.T) {
	usr, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	addr := &net.UnixAddr{Net: "unix", Name: filepath.Join(t.TempDir(), "missing.sock")}
	if err := chuser(usr.Uid, addr); !isChownError(err) {
		t.Fatalf("expected a chown error for %s, got %v", addr.Name, err)
	}
}

// Change to the current user by ID.
func TestCurrentUserid(t *testing.T) {
	usr, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	if usr.Uid != "0" {
		t.Skip("setgroups(2): Only the superuser may set new groups.")
	}

	if err = chuser(usr.Uid, nil); err != nil {
		t.Fatal(err)
	}
}

// Change to a common user by name.
func TestCommonUsername(t *testing.T) {
	usr, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	if usr.Uid != "0" {
		t.Skip("setgroups(2): Only the superuser may set new groups.")
	}

	if err := chuser("nobody", nil); err != nil {
		if _, ok := err.(user.UnknownUserError); ok {
			t.Skip(err)
		}
		t.Fatal(err)
	}
}
