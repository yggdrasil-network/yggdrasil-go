//go:build !windows
// +build !windows

package main

import (
	"unsafe"

	"github.com/webview/webview"
)

func Console(show bool) {
}

type WebView = webview.WebView

type Hint = webview.Hint

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
	return webview.New(debug)
}

// NewWindow creates a new webview using an existing window.
func NewWindow(debug bool, window unsafe.Pointer) WebView {
	return webview.NewWindow(debug, window)
}
