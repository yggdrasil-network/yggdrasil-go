package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/Arceliar/phony"
	"github.com/coder/websocket"
)

type linkWS struct {
	phony.Inbox
	*links
	listenconfig *net.ListenConfig
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
	ch            chan *linkWSConn
	ctx           context.Context
	acceptOptions *websocket.AcceptOptions
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
	if r.URL.Path == "/health" || r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
		return
	}

	c, err := websocket.Accept(w, r, s.acceptOptions)
	if err != nil {
		return
	}

	if c.Subprotocol() != "ygg-ws" {
		c.Close(websocket.StatusPolicyViolation, "client must speak the ygg-ws subprotocol")
		return
	}

	s.ch <- &linkWSConn{
		Conn: websocket.NetConn(s.ctx, c, websocket.MessageBinary),
	}
}

func (l *links) newLinkWS() *linkWS {
	lt := &linkWS{
		links: l,
		listenconfig: &net.ListenConfig{
			KeepAlive: -1,
		},
	}
	return lt
}

func wsAcceptOptions(url *url.URL) *websocket.AcceptOptions {
	opts := &websocket.AcceptOptions{
		Subprotocols: []string{"ygg-ws"},
	}
	for _, origin := range url.Query()["origin"] {
		switch origin {
		case "":
			continue
		case "*":
			opts.InsecureSkipVerify = true
			opts.OriginPatterns = nil
			return opts
		default:
			opts.OriginPatterns = append(opts.OriginPatterns, origin)
		}
	}
	return opts
}

func (l *linkWS) dial(ctx context.Context, url *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	return l.findSuitableIP(url, func(hostname string, ip net.IP, port int) (net.Conn, error) {
		u := *url
		u.Host = net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port))
		addr := &net.TCPAddr{
			IP:   ip,
			Port: port,
		}
		dialer, err := l.tcp.dialerFor(addr, info.sintf)
		if err != nil {
			return nil, err
		}
		transport := &http.Transport{
			Proxy:       http.ProxyFromEnvironment,
			Dial:        dialer.Dial, // nolint:staticcheck
			DialContext: dialer.DialContext,
		}
		// Each dial owns its transport. Closing its idle connections when
		// the dial returns also stops a dial that net/http may still be
		// finishing in the background after the request was cancelled; the
		// upgraded connection is not in the pool and is not affected.
		defer transport.CloseIdleConnections()
		wsconn, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
			HTTPClient: &http.Client{
				Transport: transport,
				// Bounds the upgrade only: the websocket library turns it
				// into a deadline for the handshake, not the connection.
				Timeout: wsDialTimeout,
			},
			Subprotocols: []string{"ygg-ws"},
			Host:         hostname,
		})
		if err != nil {
			return nil, err
		}
		return &linkWSConn{
			Conn: websocket.NetConn(ctx, wsconn, websocket.MessageBinary),
		}, nil
	})
}

func (l *linkWS) listen(ctx context.Context, url *url.URL, _ string) (net.Listener, error) {
	nl, err := l.listenconfig.Listen(ctx, "tcp", url.Host)
	if err != nil {
		return nil, err
	}

	ch := make(chan *linkWSConn)

	httpServer := &http.Server{
		Handler: &wsServer{
			ch:            ch,
			ctx:           ctx,
			acceptOptions: wsAcceptOptions(url),
		},
		BaseContext:  func(_ net.Listener) context.Context { return ctx },
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}

	lwl := &linkWSListener{
		ch:         ch,
		ctx:        ctx,
		httpServer: httpServer,
		listener:   nl,
	}
	go lwl.httpServer.Serve(nl) // nolint:errcheck
	return lwl, nil
}

// wsDialTimeout bounds the handshake stages of ws://, wss:// and socks://
// dials (TLS, HTTP upgrade, SOCKS negotiation). Without it, a peer that
// accepts the TCP connection but never completes the handshake holds the
// dial open until the peer is removed. It is a variable so that tests can
// shorten it.
var wsDialTimeout = 15 * time.Second
