package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"

	"golang.org/x/net/proxy"
)

type linkSOCKS struct {
	*links
}

func (l *links) newLinkSOCKS() *linkSOCKS {
	lt := &linkSOCKS{
		links: l,
	}
	return lt
}

func (l *linkSOCKS) dial(_ context.Context, url *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	var proxyAuth *proxy.Auth
	if url.User != nil && url.User.Username() != "" {
		proxyAuth = &proxy.Auth{
			User: url.User.Username(),
		}
		proxyAuth.Password, _ = url.User.Password()
	}
	tlsconfig := l.tls.config.Clone()
	return l.links.findSuitableIP(url, func(hostname string, ip net.IP, port int) (net.Conn, error) {
		hostport := net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port))
		dialer, err := l.tcp.dialerFor(&net.TCPAddr{
			IP:   ip,
			Port: port,
		}, info.sintf)
		if err != nil {
			return nil, err
		}
		proxy, err := proxy.SOCKS5("tcp", hostport, proxyAuth, dialer)
		if err != nil {
			return nil, err
		}
		pathtokens := strings.Split(strings.Trim(url.Path, "/"), "/")
		conn, err := proxy.Dial("tcp", pathtokens[0])
		if err != nil {
			return nil, err
		}
		if url.Scheme == "sockstls" {
			tlsconfig.ServerName = hostname
			if sni := options.tlsSNI; sni != "" {
				tlsconfig.ServerName = sni
			}
			conn = tls.Client(conn, tlsconfig)
		}
		return conn, nil
	})
}

func (l *linkSOCKS) listen(ctx context.Context, url *url.URL, _ string) (net.Listener, error) {
	return nil, fmt.Errorf("SOCKS listener not supported")
}
