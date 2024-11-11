//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris
// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris

package main

import (
	"testing"
	"os/user"
)

// Usernames must not contain a number sign.
func TestEmptyString (t *testing.T) {
	if chuser("") == nil {
		t.Fatal("the empty string is not a valid user")
	}
}

// Either omit delimiter and group, or omit both.
func TestEmptyGroup (t *testing.T) {
	if chuser("0:") == nil {
		t.Fatal("the empty group is not allowed")
	}
}

// Either user only or user and group.
func TestGroupOnly (t *testing.T) {
	if chuser(":0") == nil {
		t.Fatal("group only is not allowed")
	}
}

// Usenames must not contain the number sign.
func TestInvalidUsername (t *testing.T) {
	const username = "#user"
	if chuser(username) == nil {
		t.Fatalf("'%s' is not a valid username", username)
	}
}

// User IDs must be non-negative.
func TestInvalidUserid (t *testing.T) {
	if chuser("-1") == nil {
		t.Fatal("User ID cannot be negative")
	}
}

// Change to the current user by ID.
func TestCurrentUserid (t *testing.T) {
	usr, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	if usr.Uid != "0" {
		t.Skip("setgroups(2): Only the superuser may set new groups.")
	}

	if err = chuser(usr.Uid); err != nil {
		t.Fatal(err)
	}
}

// Change to a common user by name.
func TestCommonUsername (t *testing.T) {
	usr, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	if usr.Uid != "0" {
		t.Skip("setgroups(2): Only the superuser may set new groups.")
	}

	if err := chuser("nobody"); err != nil {
		if _, ok := err.(user.UnknownUserError); ok {
			t.Skip(err)
		}
		t.Fatal(err)
	}
}
