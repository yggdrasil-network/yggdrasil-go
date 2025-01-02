package core

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/Arceliar/phony"
	"github.com/mdlayher/vsock"
)

type linkVSOCK struct {
	phony.Inbox
	*links
}

func (l *links) newLinkVSOCK() *linkVSOCK {
	lt := &linkVSOCK{
		links: l,
	}
	return lt
}

func (l *linkVSOCK) dial(ctx context.Context, url *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	localPort, err := strconv.ParseUint(url.Port(), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("no VSOCK port specified: %w", err)
	}
	contextID, err := urlParseContextID(url)
	if err != nil {
		return nil, fmt.Errorf("Unknown VSOCK host and cannot parse as numerical contextID: %w", err)
	}
	return vsock.Dial(contextID, uint32(localPort), nil)
}

func (l *linkVSOCK) listen(ctx context.Context, url *url.URL, _ string) (net.Listener, error) {
	localPort, err := strconv.ParseUint(url.Port(), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("no VSOCK port specified: %w", err)
	}
	contextID, err := urlParseContextID(url)
	if err != nil {
		return nil, fmt.Errorf("Unknown VSOCK host and cannot parse as numerical contextID: %w", err)
	}
	return vsock.ListenContextID(contextID, uint32(localPort), nil)
}

func urlParseContextID(u *url.URL) (uint32, error) {
	var contextID uint32

	switch strings.ToLower(u.Hostname()) {
	case "hypervisor":
		contextID = vsock.Hypervisor
	case "local":
		contextID = vsock.Local
	case "host":
		contextID = vsock.Host
	default:
		parsedHost, err := strconv.ParseUint(u.Hostname(), 10, 32)
		if err != nil {
			return 0, err
		}
		contextID = uint32(parsedHost)
	}
	return contextID, nil
}
