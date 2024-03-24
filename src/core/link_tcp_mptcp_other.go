//go:build !go1.21
// +build !go1.21

package core

import "net"

func setMPTCPForDialer(d *net.Dialer) {
	// Not supported on versions under Go 1.21
}

func setMPTCPForListener(lc *net.ListenConfig) {
	// Not supported on versions under Go 1.21
}

func isMPTCP(c net.Conn) bool {
	return false
}
