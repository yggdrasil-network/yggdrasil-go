package core

import (
	"context"
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
	dialer, err := proxy.SOCKS5("tcp", url.Host, proxyAuth, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("failed to configure proxy")
	}
	pathtokens := strings.Split(strings.Trim(url.Path, "/"), "/")
	return dialer.Dial("tcp", pathtokens[0])
}

func (l *linkSOCKS) listen(ctx context.Context, url *url.URL, _ string) (net.Listener, error) {
	return nil, fmt.Errorf("SOCKS listener not supported")
}
