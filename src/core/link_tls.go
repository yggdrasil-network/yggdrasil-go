package core

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"

	"github.com/Arceliar/phony"
)

type linkTLS struct {
	phony.Inbox
	*links
	tcp        *linkTCP
	listener   *net.ListenConfig
	config     *tls.Config
	_listeners map[*Listener]context.CancelFunc
}

func (l *links) newLinkTLS(tcp *linkTCP) *linkTLS {
	lt := &linkTLS{
		links: l,
		tcp:   tcp,
		listener: &net.ListenConfig{
			Control:   tcp.tcpContext,
			KeepAlive: -1,
		},
		config:     l.core.config.tls.Clone(),
		_listeners: map[*Listener]context.CancelFunc{},
	}
	return lt
}

func (l *linkTLS) dial(ctx context.Context, url *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	dialers, err := l.tcp.dialersFor(url, info, options)
	if err != nil {
		return nil, err
	}
	if len(dialers) == 0 {
		return nil, nil
	}
	for _, d := range dialers {
		tlsconfig := l.config.Clone()
		tlsconfig.ServerName = options.tlsSNI
		tlsdialer := &tls.Dialer{
			NetDialer: d.dialer,
			Config:    tlsconfig,
		}
		var conn net.Conn
		conn, err = tlsdialer.DialContext(ctx, "tcp", d.addr.String())
		if err != nil {
			continue
		}
		return conn, nil
	}
	return nil, err
}

func (l *linkTLS) listen(ctx context.Context, url *url.URL, sintf string, options linkOptions) (net.Listener, error) {
	listener, err := l.tcp.listen(ctx, url, sintf, options)
	if err != nil {
		return nil, err
	}
	return tls.NewListener(listener, l.config), nil
}
