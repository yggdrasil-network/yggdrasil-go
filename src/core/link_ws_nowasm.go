//go:build !wasm

package core

import (
	"context"
	"net"
	"net/http"
	"github.com/coder/websocket"
)

func setWSDialOptions(options *websocket.DialOptions, dialer any, hostname string) {
	d := dialer.(interface{
		Dial(network, address string) (net.Conn, error)
		DialContext(ctx context.Context, network, address string) (net.Conn, error)
	})
	options.Host = hostname
	options.HTTPClient = &http.Client{
		Transport: &http.Transport{
			Proxy:       http.ProxyFromEnvironment,
			Dial:        d.Dial,
			DialContext: d.DialContext,
		},
	}
}
