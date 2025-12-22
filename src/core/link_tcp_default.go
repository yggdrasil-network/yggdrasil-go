//go:build !android && !ios

package core

import (
	"fmt"
	"net"
)

// handleInterfaceError handles interface lookup errors on desktop platforms.
// On desktop systems, InterfaceByName failure is a genuine error that should
// be reported, as these platforms don't have the same restrictions as mobile.
func (l *linkTCP) handleInterfaceError(err error, sintf string, dst *net.TCPAddr, dialer *net.Dialer) (*net.Dialer, error) {
	return nil, fmt.Errorf("interface %q not found: %w", sintf, err)
}
