//go:build !windows
// +build !windows

package main

import "os"

func get_user_home_path() string {
	path, exists := os.LookupEnv("HOME")
	if exists {
		return path
	} else {
		return ""
	}
}

func Console(show bool) {
}
