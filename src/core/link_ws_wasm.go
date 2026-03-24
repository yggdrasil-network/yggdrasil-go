//go:build wasm

package core

import (
	"github.com/coder/websocket"
)

func setWSDialOptions(options *websocket.DialOptions, dialer any, hostname string) {
	// wasm implementation of coder/websocket doesn't support Host or HTTPClient
}
