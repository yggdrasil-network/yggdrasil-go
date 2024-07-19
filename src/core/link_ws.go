package core

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/Arceliar/phony"
	"nhooyr.io/websocket"
)

type linkWS struct {
	phony.Inbox
	*links
}

type linkWSConn struct {
	net.Conn
}

type linkWSListener struct {
	ch         chan *linkWSConn
	ctx        context.Context
	httpServer *http.Server
	listener   net.Listener
}

type wsServer struct {
	ch  chan *linkWSConn
	ctx context.Context
}

func (l *linkWSListener) Accept() (net.Conn, error) {
	qs := <-l.ch
	if qs == nil {
		return nil, context.Canceled
	}
	return qs, nil
}

func (l *linkWSListener) Addr() net.Addr {
	return l.listener.Addr()
}

func (l *linkWSListener) Close() error {
	if err := l.httpServer.Shutdown(l.ctx); err != nil {
		return err
	}

	return l.listener.Close()
}

func (s *wsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{"ygg-ws"},
	})

	if err != nil {
		return
	}

	if c.Subprotocol() != "ygg-ws" {
		c.Close(websocket.StatusPolicyViolation, "client must speak the ygg-ws subprotocol")
		return
	}

	netconn := websocket.NetConn(s.ctx, c, websocket.MessageBinary)

	ch := s.ch
	ch <- &linkWSConn{
		Conn: netconn,
	}
}

func (l *links) newLinkWS() *linkWS {
	lt := &linkWS{
		links: l,
	}

	return lt
}

func (l *linkWS) dial(ctx context.Context, url *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	wsconn, _, err := websocket.Dial(ctx, url.String(), &websocket.DialOptions{
		Subprotocols: []string{"ygg-ws"},
	})
	if err != nil {
		return nil, err
	}
	netconn := websocket.NetConn(ctx, wsconn, websocket.MessageBinary)
	return &linkWSConn{
		Conn: netconn,
	}, nil
}

func (l *linkWS) listen(ctx context.Context, url *url.URL, _ string) (net.Listener, error) {
	nl, err := net.Listen("tcp", url.Host)
	if err != nil {
		return nil, err
	}

	ch := make(chan *linkWSConn)

	httpServer := &http.Server{
		Handler: &wsServer{
			ch:  ch,
			ctx: ctx,
		},
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}

	lwl := &linkWSListener{
		ch:         ch,
		ctx:        ctx,
		httpServer: httpServer,
		listener:   nl,
	}
	go lwl.httpServer.Serve(nl)
	return lwl, nil
}
