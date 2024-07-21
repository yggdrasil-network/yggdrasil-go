package core

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/Arceliar/phony"
	"nhooyr.io/websocket"
)

type linkWSS struct {
	phony.Inbox
	tlsconfig *tls.Config
	*links
}

type linkWSSConn struct {
	net.Conn
}

type linkWSSListener struct {
	ch          chan *linkWSSConn
	ctx         context.Context
	httpServer  *http.Server
	listener    net.Listener
	tlslistener net.Listener
}

type wssServer struct {
	ch  chan *linkWSSConn
	ctx context.Context
}

func (l *linkWSSListener) Accept() (net.Conn, error) {
	qs := <-l.ch
	if qs == nil {
		return nil, context.Canceled
	}
	return qs, nil
}

func (l *linkWSSListener) Addr() net.Addr {
	return l.listener.Addr()
}

func (l *linkWSSListener) Close() error {
	if err := l.httpServer.Shutdown(l.ctx); err != nil {
		return err
	}
	if err := l.tlslistener.Close(); err != nil {
		return err
	}
	return l.listener.Close()
}

func (s *wssServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/h" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return
	}
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
	ch <- &linkWSSConn{
		Conn: netconn,
	}
}

func (l *links) newLinkWSS() *linkWSS {
	lwss := &linkWSS{
		links:     l,
		tlsconfig: l.core.config.tls.Clone(),
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
	nl, err := net.Listen("tcp", url.Host)
	if err != nil {
		return nil, err
	}

	tl := tls.NewListener(nl, l.tlsconfig)

	ch := make(chan *linkWSSConn)

	httpServer := &http.Server{
		Handler: &wssServer{
			ch:  ch,
			ctx: ctx,
		},
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}

	lwl := &linkWSSListener{
		ch:          ch,
		ctx:         ctx,
		httpServer:  httpServer,
		listener:    nl,
		tlslistener: tl,
	}
	go lwl.httpServer.Serve(tl)
	return lwl, nil
}
