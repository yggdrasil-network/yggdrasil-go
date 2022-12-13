//go:build windows
// +build windows

package main

import (
	"os"
	"syscall"
)

func get_user_home_path() string {
	path, exists := os.LookupEnv("USERPROFILE")
	if exists {
		return path
	} else {
		return ""
	}
}

func Console(show bool) {
	var getWin = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow")
	var showWin = syscall.NewLazyDLL("user32.dll").NewProc("ShowWindow")
	hwnd, _, _ := getWin.Call()
	if hwnd == 0 {
		return
	}
	if show {
		var SW_RESTORE uintptr = 9
		showWin.Call(hwnd, SW_RESTORE)
	} else {
		var SW_HIDE uintptr = 0
		showWin.Call(hwnd, SW_HIDE)
	}
}
