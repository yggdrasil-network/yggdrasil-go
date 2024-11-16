package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"

	"github.com/Arceliar/phony"
)

type linkTLS struct {
	phony.Inbox
	*links
	tcp      *linkTCP
	listener *net.ListenConfig
	config   *tls.Config
}

func (l *links) newLinkTLS(tcp *linkTCP) *linkTLS {
	lt := &linkTLS{
		links: l,
		tcp:   tcp,
		listener: &net.ListenConfig{
			Control:   tcp.tcpContext,
			KeepAlive: -1,
		},
		config: l.core.config.tls.Clone(),
	}
	return lt
}

func (l *linkTLS) dial(ctx context.Context, url *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	tlsconfig := l.config.Clone()
	return l.links.findSuitableIP(url, func(hostname string, ip net.IP, port int) (net.Conn, error) {
		tlsconfig.ServerName = hostname
		if sni := options.tlsSNI; sni != "" {
			tlsconfig.ServerName = sni
		}
		addr := &net.TCPAddr{
			IP:   ip,
			Port: port,
		}
		dialer, err := l.tcp.dialerFor(addr, info.sintf)
		if err != nil {
			return nil, err
		}
		tlsdialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    tlsconfig,
		}
		return tlsdialer.DialContext(ctx, "tcp", addr.String())
	})
}

func (l *linkTLS) listen(ctx context.Context, url *url.URL, sintf string) (net.Listener, error) {
	hostport := url.Host
	if sintf != "" {
		if host, port, err := net.SplitHostPort(hostport); err == nil {
			hostport = fmt.Sprintf("[%s%%%s]:%s", host, sintf, port)
		}
	}
	listener, err := l.listener.Listen(ctx, "tcp", hostport)
	if err != nil {
		return nil, err
	}
	tlslistener := tls.NewListener(listener, l.config)
	return tlslistener, nil
}
