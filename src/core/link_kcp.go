package core

import (
	"context"
	"net"
	"net/url"

	"github.com/Arceliar/phony"
	"github.com/xtaci/kcp-go/v5"
)

type linkKCP struct {
	phony.Inbox
	*links
}

func (l *links) newLinkKCP() *linkKCP {
	return &linkKCP{
		links: l,
	}
}

func (l *linkKCP) dial(ctx context.Context, url *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	conn, err := kcp.DialWithOptions(url.Host, nil, 10, 3)
	if err != nil {
		return nil, err
	}

	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		}
	}()

	return conn, nil
}

func (l *linkKCP) listen(ctx context.Context, url *url.URL, _ string) (net.Listener, error) {
	lis, err := kcp.ListenWithOptions(url.Host, nil, 10, 3)
	if err != nil {
		return nil, err
	}

	go func() {
		select {
		case <-ctx.Done():
			lis.Close()
		}
	}()

	return lis, nil
}
