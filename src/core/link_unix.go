package core

import (
	"context"
	"net"
	"net/url"
	"time"

	"github.com/Arceliar/phony"
)

type linkUNIX struct {
	phony.Inbox
	*links
	dialer   *net.Dialer
	listener *net.ListenConfig
}

func (l *links) newLinkUNIX() *linkUNIX {
	lt := &linkUNIX{
		links: l,
		dialer: &net.Dialer{
			Timeout:   time.Second * 5,
			KeepAlive: -1,
		},
		listener: &net.ListenConfig{
			KeepAlive: -1,
		},
	}
	return lt
}

func (l *linkUNIX) dial(ctx context.Context, url *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	addr, err := net.ResolveUnixAddr("unix", url.Path)
	if err != nil {
		return nil, err
	}
	return l.dialer.DialContext(ctx, "unix", addr.String())
}

func (l *linkUNIX) listen(ctx context.Context, url *url.URL, _ string) (net.Listener, error) {
	return l.listener.Listen(ctx, "unix", url.Path)
}
