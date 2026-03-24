//go:build wasm

package core

import (
	"crypto/tls"
	"github.com/coder/websocket"
)

func setWSSDialOptions(options *websocket.DialOptions, dialer any, hostname string, tlsconfig *tls.Config) {
	// wasm implementation of coder/websocket doesn't support Host or HTTPClient
}
