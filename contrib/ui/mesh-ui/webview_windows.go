//go:build windows
// +build windows

package main

import (
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2"
)

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

type WebView = webview2.WebView

type Hint = webview2.Hint

const (
	// HintNone specifies that width and height are default size
	HintNone Hint = iota

	// HintFixed specifies that window size can not be changed by a user
	HintFixed

	// HintMin specifies that width and height are minimum bounds
	HintMin

	// HintMax specifies that width and height are maximum bounds
	HintMax
)

// New creates a new webview in a new window.
func New(debug bool) WebView {
	//return webview2.New(debug)
	return webview2.NewWithOptions(webview2.WebViewOptions{
		Debug: debug,
		WindowOptions: webview2.WindowOptions{
			IconId: 101,
			Title:  "RiV-mesh",
			Width:  706,
			Height: 960,
		},
	})
}

// NewWindow creates a new webview using an existing window.
func NewWindow(debug bool, window unsafe.Pointer) WebView {
	return webview2.NewWindow(debug, window)
}
