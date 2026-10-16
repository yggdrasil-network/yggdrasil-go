package core

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

func TestWSAcceptOptionsOriginQuery(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name               string
		rawurl             string
		insecureSkipVerify bool
		originPatterns     []string
	}{
		{
			name:   "default same origin policy",
			rawurl: "ws://0.0.0.0:9001",
		},
		{
			name:           "host origin pattern",
			rawurl:         "ws://0.0.0.0:9001?origin=demo.example.org",
			originPatterns: []string{"demo.example.org"},
		},
		{
			name:           "scheme origin pattern",
			rawurl:         "ws://0.0.0.0:9001?origin=https%3A%2F%2Fdemo.example.org",
			originPatterns: []string{"https://demo.example.org"},
		},
		{
			name:           "multiple origin patterns",
			rawurl:         "ws://0.0.0.0:9001?origin=demo.example.org&origin=https%3A%2F%2Fdemo2.example.org",
			originPatterns: []string{"demo.example.org", "https://demo2.example.org"},
		},
		{
			name:               "wildcard disables verification",
			rawurl:             "ws://0.0.0.0:9001?origin=*",
			insecureSkipVerify: true,
		},
		{
			name:               "wildcard overrides other patterns",
			rawurl:             "ws://0.0.0.0:9001?origin=demo.example.org&origin=*",
			insecureSkipVerify: true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tc.rawurl)
			if err != nil {
				t.Fatal(err)
			}

			opts := wsAcceptOptions(u)
			if got := opts.InsecureSkipVerify; got != tc.insecureSkipVerify {
				t.Fatalf("InsecureSkipVerify = %v, want %v", got, tc.insecureSkipVerify)
			}
			if strings.Join(opts.OriginPatterns, ",") != strings.Join(tc.originPatterns, ",") {
				t.Fatalf("OriginPatterns = %#v, want %#v", opts.OriginPatterns, tc.originPatterns)
			}
			if strings.Join(opts.Subprotocols, ",") != "ygg-ws" {
				t.Fatalf("Subprotocols = %#v, want [ygg-ws]", opts.Subprotocols)
			}
		})
	}
}

func TestWSServerOriginPolicy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		rawurl  string
		origin  string
		success bool
	}{
		{
			name:    "default rejects cross origin",
			rawurl:  "ws://127.0.0.1:0",
			origin:  "https://demo.example.org",
			success: false,
		},
		{
			name:    "configured origin accepts cross origin",
			rawurl:  "ws://127.0.0.1:0?origin=demo.example.org",
			origin:  "https://demo.example.org",
			success: true,
		},
		{
			name:    "wildcard accepts cross origin",
			rawurl:  "ws://127.0.0.1:0?origin=*",
			origin:  "https://unexpected.example.org",
			success: true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tc.rawurl)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ch := make(chan *linkWSConn, 1)
			server := httptest.NewServer(&wsServer{
				ch:            ch,
				ctx:           ctx,
				acceptOptions: wsAcceptOptions(u),
			})
			defer server.Close()

			dialURL := "ws" + strings.TrimPrefix(server.URL, "http")
			c, resp, err := websocket.Dial(ctx, dialURL, &websocket.DialOptions{
				HTTPHeader: http.Header{
					"Origin": []string{tc.origin},
				},
				Subprotocols: []string{"ygg-ws"},
			})
			if err != nil && resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if tc.success {
				if err != nil {
					t.Fatalf("websocket dial failed: %v", err)
				}
				_ = c.Close(websocket.StatusNormalClosure, "")
				select {
				case conn := <-ch:
					_ = conn.Close()
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for accepted connection")
				}
			} else if err == nil {
				_ = c.Close(websocket.StatusNormalClosure, "")
				t.Fatal("websocket dial succeeded, want origin rejection")
			}
		})
	}
}

// frozenHandshakeServer accepts TCP connections and never answers, so any
// handshake on top of them stalls. It reports when a connection arrives and
// when the client closes it.
func frozenHandshakeServer(t *testing.T, accepted chan<- time.Time, closed chan<- time.Time) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- time.Now()
			buf := make([]byte, 256)
			for {
				if _, err := c.Read(buf); err != nil {
					break // the client went away
				}
			}
			closed <- time.Now()
			_ = c.Close()
		}
	}()
	return ln
}

// newDialTestNode starts a node for dialling tests and stops it at the end.
func newDialTestNode(t *testing.T) *Core {
	t.Helper()
	cfg := config.GenerateConfig()
	if err := cfg.GenerateSelfSignedCertificate(); err != nil {
		t.Fatal(err)
	}
	node, err := New(cfg.Certificate, GetLoggerWithPrefix("", false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(node.Stop)
	return node
}

// testDialBounded dials a peer that never completes the handshake and
// requires the dial to be abandoned at wsDialTimeout.
func testDialBounded(t *testing.T, scheme string) {
	t.Helper()
	old := wsDialTimeout
	wsDialTimeout = time.Second
	t.Cleanup(func() { wsDialTimeout = old })

	accepted := make(chan time.Time, 4)
	closed := make(chan time.Time, 4)
	ln := frozenHandshakeServer(t, accepted, closed)
	defer ln.Close()

	node := newDialTestNode(t)
	raw := scheme + "://" + ln.Addr().String()
	if scheme == "socks" {
		// The frozen server is the proxy: the greeting never completes.
		raw += "/127.0.0.1:1"
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := node.CallPeer(u, ""); err != nil {
		t.Fatal(err)
	}
	var ta time.Time
	select {
	case ta = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("no dial attempt arrived")
	}
	select {
	case tc := <-closed:
		d := tc.Sub(ta)
		if d < wsDialTimeout/2 {
			t.Fatalf("%s dial abandoned after %v, before the %v handshake timeout fired", scheme, d, wsDialTimeout)
		}
		if d > wsDialTimeout+4*time.Second {
			t.Fatalf("%s dial abandoned only after %v, want about %v", scheme, d, wsDialTimeout)
		}
	case <-time.After(wsDialTimeout + 5*time.Second):
		t.Fatalf("%s dial was not abandoned within %v", scheme, wsDialTimeout)
	}
}

func TestWSDialTimeout(t *testing.T) {
	testDialBounded(t, "ws")
}

func TestWSSDialTimeout(t *testing.T) {
	testDialBounded(t, "wss")
}

func TestSOCKSDialTimeout(t *testing.T) {
	testDialBounded(t, "socks")
}

// TestWSDialTimeoutSparesUpgradedConn checks that wsDialTimeout bounds only
// the handshake: an upgraded connection keeps working after it has passed.
func TestWSDialTimeoutSparesUpgradedConn(t *testing.T) {
	for _, scheme := range []string{"ws", "wss"} {
		t.Run(scheme, func(t *testing.T) {
			old := wsDialTimeout
			wsDialTimeout = 200 * time.Millisecond
			t.Cleanup(func() { wsDialTimeout = old })

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"ygg-ws"}})
				if err != nil {
					return
				}
				defer func() {
					if err := ws.CloseNow(); err != nil {
						t.Logf("closing the server side: %v", err)
					}
				}()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				mt, data, err := ws.Read(ctx)
				if err != nil {
					return
				}
				if err := ws.Write(ctx, mt, data); err != nil {
					t.Logf("echo: %v", err)
				}
			})
			var srv *httptest.Server
			if scheme == "wss" {
				srv = httptest.NewTLSServer(handler)
			} else {
				srv = httptest.NewServer(handler)
			}
			defer srv.Close()

			node := newDialTestNode(t)
			u, err := url.Parse(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			u.Scheme = scheme
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var c net.Conn
			if scheme == "wss" {
				c, err = node.links.wss.dial(ctx, u, linkInfo{}, linkOptions{})
			} else {
				c, err = node.links.ws.dial(ctx, u, linkInfo{}, linkOptions{})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()

			time.Sleep(2 * wsDialTimeout)
			if err := c.SetDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Write([]byte("alive")); err != nil {
				t.Fatalf("upgraded connection did not outlive the handshake timeout: %v", err)
			}
			reply := make([]byte, 5)
			if _, err := io.ReadFull(c, reply); err != nil {
				t.Fatal(err)
			}
			if string(reply) != "alive" {
				t.Fatalf("echo mismatch: %q", reply)
			}
		})
	}
}

// TestDialCancelClosesSocket cancels dials stuck in the handshake and
// requires them to return at once and close their socket, rather than
// leaving it open until the handshake timeout.
func TestDialCancelClosesSocket(t *testing.T) {
	for _, scheme := range []string{"ws", "wss", "socks"} {
		t.Run(scheme, func(t *testing.T) {
			accepted := make(chan time.Time, 4)
			closed := make(chan time.Time, 4)
			ln := frozenHandshakeServer(t, accepted, closed)
			defer ln.Close()

			node := newDialTestNode(t)
			raw := scheme + "://" + ln.Addr().String()
			if scheme == "socks" {
				raw += "/127.0.0.1:1"
			}
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var c net.Conn
				var err error
				switch scheme {
				case "ws":
					c, err = node.links.ws.dial(ctx, u, linkInfo{}, linkOptions{})
				case "wss":
					c, err = node.links.wss.dial(ctx, u, linkInfo{}, linkOptions{})
				default:
					c, err = node.links.socks.dial(ctx, u, linkInfo{}, linkOptions{})
				}
				if c != nil {
					_ = c.Close()
				}
				done <- err
			}()
			select {
			case <-accepted:
			case <-time.After(2 * time.Second):
				t.Fatal("no dial attempt arrived")
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancelled dial succeeded")
				}
				if !errors.Is(err, context.Canceled) {
					// The SOCKS dialer interrupts the negotiation by setting
					// an expired deadline, so it may report a timeout.
					var ne net.Error
					if scheme != "socks" || !errors.As(err, &ne) || !ne.Timeout() {
						t.Fatalf("unexpected error from the cancelled dial: %v", err)
					}
				}
			case <-time.After(time.Second):
				t.Fatal("dial did not return after cancellation")
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("the cancelled dial left its socket open")
			}
		})
	}
}
