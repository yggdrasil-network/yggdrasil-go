//go:build android || ios

package core

import (
	"fmt"
	"net"
)

// handleInterfaceError handles interface lookup errors on mobile platforms.
// On Android/iOS, InterfaceByName may fail due to permission restrictions
// (SELinux on Android), but for link-local addresses with zone identifiers,
// the zone is sufficient for routing and we can proceed without source binding.
func (l *linkTCP) handleInterfaceError(err error, sintf string, dst *net.TCPAddr, dialer *net.Dialer) (*net.Dialer, error) {
	// For link-local addresses with zone set, we can proceed without binding
	// The zone identifier tells the kernel which interface to use
	if dst.IP.IsLinkLocalUnicast() && dst.Zone != "" {
		return dialer, nil
	}
	// For other cases, return the original error
	return nil, fmt.Errorf("interface %q not found: %w", sintf, err)
}
