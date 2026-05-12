package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
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
