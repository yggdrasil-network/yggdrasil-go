// +build !darwin

package yggdrasil

import (
	"syscall"
)

// WARNING: This context is used both by net.Dialer and net.Listen in tcp.go

func (iface *tcpInterface) tcpContext(network, address string, c syscall.RawConn) error {
	return nil
}
