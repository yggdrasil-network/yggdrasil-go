package core

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/Arceliar/phony"
	"nhooyr.io/websocket"
)

type linkWSS struct {
	phony.Inbox
	*links
}

type linkWSSConn struct {
	net.Conn
}

func (l *links) newLinkWSS() *linkWSS {
	lwss := &linkWSS{
		links:     l,
	}

	return lwss
}

func (l *linkWSS) dial(ctx context.Context, url *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	wsconn, _, err := websocket.Dial(ctx, url.String(), &websocket.DialOptions{
		Subprotocols: []string{"ygg-ws"},
	})
	if err != nil {
		return nil, err
	}
	netconn := websocket.NetConn(ctx, wsconn, websocket.MessageBinary)
	return &linkWSSConn{
		Conn: netconn,
	}, nil
}

func (l *linkWSS) listen(ctx context.Context, url *url.URL, _ string) (net.Listener, error) {
	return nil, fmt.Errorf("WSS listener not supported, use WS listener behind reverse proxy instead")
}
