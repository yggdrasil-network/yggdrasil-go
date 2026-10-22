package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gologme/log"
)

// newTestAdmin returns an AdminSocket that is usable for connection-level
// tests without a core.Core: no handlers are registered by default.
func newTestAdmin(t *testing.T) *AdminSocket {
	t.Helper()
	return &AdminSocket{
		log:      log.New(io.Discard, "", 0),
		handlers: make(map[string]handler),
	}
}

// newTestConn returns a connected pair of unix sockets. A unix socket is used
// rather than net.Pipe because the tests need half-close semantics.
func newTestConn(t *testing.T) (client, server net.Conn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "admin.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			close(accepted)
			return
		}
		accepted <- c
	}()
	client, err = net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	server, ok := <-accepted
	if !ok {
		t.Fatal("accept failed")
	}
	t.Cleanup(func() { _ = server.Close() })
	return client, server
}

// countingConn counts the bytes read from the underlying connection, which is
// how the tests observe how much of an oversized request was actually consumed.
type countingConn struct {
	net.Conn
	read atomic.Int64
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.read.Add(int64(n))
	return n, err
}

// decodeResponse reads a single response from the client side of a connection.
func decodeResponse(t *testing.T, conn net.Conn, timeout time.Duration) AdminSocketResponse {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var resp AdminSocketResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

// TestOversizedRequestRejected checks that a request larger than maxRequestSize
// is rejected with an error and that the decoder stops reading at the limit
// instead of buffering the whole payload.
func TestOversizedRequestRejected(t *testing.T) {
	client, server := newTestConn(t)
	counter := &countingConn{Conn: server}
	a := newTestAdmin(t)
	go a.handleRequest(counter)

	// An unterminated JSON string keeps the decoder scanning for the closing
	// quote, so it will keep reading until it hits the limit. Sending plain
	// garbage would fail the parser on the first byte instead.
	payload := append([]byte(`{"request":"`), bytes.Repeat([]byte("A"), int(maxRequestSize)+1<<20)...)
	writeDone := make(chan error, 1)
	go func() {
		_, err := client.Write(payload)
		if u, ok := client.(*net.UnixConn); ok {
			// Let the peer see EOF once the payload is written, so that a
			// connection without a limit fails fast instead of blocking.
			if err == nil {
				err = u.CloseWrite()
			}
		}
		writeDone <- err
	}()

	resp := decodeResponse(t, client, 10*time.Second)

	if resp.Status != "error" {
		t.Errorf("status = %q, want %q", resp.Status, "error")
	}
	if !strings.Contains(resp.Error, "too large") {
		t.Errorf("error = %q, want it to mention that the request is too large", resp.Error)
	}
	if got := counter.read.Load(); got > maxRequestSize {
		t.Errorf("read %d bytes from the connection, want at most maxRequestSize (%d)", got, maxRequestSize)
	}
	if got := counter.read.Load(); got == 0 {
		t.Error("read 0 bytes from the connection, want the request to be partially read")
	}
	<-writeDone
}

// TestIdleConnectionTimesOut checks that a connection which sends nothing is
// closed after requestReadTimeout and gets an error response.
func TestIdleConnectionTimesOut(t *testing.T) {
	oldTimeout := requestReadTimeout
	requestReadTimeout = 200 * time.Millisecond
	defer func() { requestReadTimeout = oldTimeout }()

	client, server := newTestConn(t)
	a := newTestAdmin(t)
	go a.handleRequest(server)

	start := time.Now()
	resp := decodeResponse(t, client, 10*time.Second)
	elapsed := time.Since(start)

	if resp.Status != "error" {
		t.Errorf("status = %q, want %q", resp.Status, "error")
	}
	if !strings.Contains(resp.Error, "timed out") {
		t.Errorf("error = %q, want it to mention the timeout", resp.Error)
	}
	if elapsed < requestReadTimeout/2 {
		t.Errorf("connection failed after %v, want it to wait for the %v deadline", elapsed, requestReadTimeout)
	}
	if elapsed > 5*time.Second {
		t.Errorf("connection took %v to fail, want it closed at the deadline", elapsed)
	}

	// The server must close the connection after the error response.
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, err := client.Read(make([]byte, 1)); err != io.EOF {
		t.Errorf("read after error response = %v, want io.EOF", err)
	}
}

// TestNormalRequestStillWorks checks that ordinary requests, including
// keepalive requests on a reused connection, are unaffected by the limits.
func TestNormalRequestStillWorks(t *testing.T) {
	client, server := newTestConn(t)
	a := newTestAdmin(t)
	var calls atomic.Int64
	a.handlers["ping"] = handler{
		desc: "test handler",
		handler: func(in json.RawMessage) (interface{}, error) {
			calls.Add(1)
			return map[string]string{"pong": "ok"}, nil
		},
	}
	go a.handleRequest(server)

	if _, err := client.Write([]byte(`{"request":"ping","keepalive":true}`)); err != nil {
		t.Fatalf("write first request: %v", err)
	}
	resp := decodeResponse(t, client, 5*time.Second)
	if resp.Status != "success" {
		t.Fatalf("first request status = %q (error %q), want %q", resp.Status, resp.Error, "success")
	}
	if resp.Request.Name != "ping" {
		t.Errorf("first request name = %q, want %q", resp.Request.Name, "ping")
	}
	var result map[string]string
	if err := json.Unmarshal(resp.Response, &result); err != nil {
		t.Errorf("first request response = %s, want valid JSON: %v", resp.Response, err)
	} else if result["pong"] != "ok" {
		t.Errorf("first request response = %s, want the handler result", resp.Response)
	}

	// A second request over the same connection must still work, i.e. the read
	// deadline is re-armed and the decoder is not stuck after the limit.
	if _, err := client.Write([]byte(`{"request":"ping"}`)); err != nil {
		t.Fatalf("write second request: %v", err)
	}
	resp = decodeResponse(t, client, 5*time.Second)
	if resp.Status != "success" {
		t.Fatalf("second request status = %q (error %q), want %q", resp.Status, resp.Error, "success")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("handler calls = %d, want 2", got)
	}
}
