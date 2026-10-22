package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
)

func newTestQUICTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "yggdrasil-quic-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	cert := tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}
	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
	}
}

func testQUICConfig() *quic.Config {
	return &quic.Config{
		MaxIdleTimeout:  time.Minute,
		KeepAlivePeriod: 20 * time.Second,
		TokenStore:      quic.NewLRUTokenStore(255, 255),
	}
}

// TestQUICListenerPeerWithoutStream verifies that a peer which completes the
// QUIC handshake but never opens a link stream does not prevent the listener
// from accepting other peers.
func TestQUICListenerPeerWithoutStream(t *testing.T) {
	tlsconfig := newTestQUICTLSConfig(t)
	l := &linkQUIC{
		tlsconfig:  tlsconfig,
		quicconfig: testQUICConfig(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := l.listen(ctx, &url.URL{Scheme: "quic", Host: "127.0.0.1:0"}, "")
	if err != nil {
		t.Fatalf("starting listener: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	dial := func() *quic.Conn {
		t.Helper()
		dctx, dcancel := context.WithTimeout(ctx, 10*time.Second)
		defer dcancel()
		conn, err := quic.DialAddr(dctx, addr, tlsconfig, testQUICConfig())
		if err != nil {
			t.Fatalf("dialing %s: %v", addr, err)
		}
		t.Cleanup(func() { _ = conn.CloseWithError(0, "") })
		return conn
	}

	// First peer completes the handshake but never opens a stream. Give the
	// listener a moment to move on to waiting for that stream, which is what
	// the old single-goroutine accept loop did before it could accept again.
	_ = dial()
	time.Sleep(500 * time.Millisecond)

	// Second peer behaves like a real yggdrasil peer: handshake, then open the
	// link stream immediately.
	peer := dial()
	sctx, scancel := context.WithTimeout(ctx, 5*time.Second)
	defer scancel()
	stream, err := peer.OpenStreamSync(sctx)
	if err != nil {
		t.Fatalf("opening stream: %v", err)
	}
	// A real yggdrasil peer starts writing its handshake as soon as the stream
	// is open, which is what makes the stream visible to the remote side.
	if _, err := stream.Write([]byte("ping")); err != nil {
		t.Fatalf("writing to stream: %v", err)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- conn
	}()

	select {
	case conn := <-accepted:
		defer conn.Close()
		// Make sure the accepted connection really carries the stream that the
		// second peer opened.
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Fatalf("reading from accepted connection: %v", err)
		}
		if string(buf) != "ping" {
			t.Fatalf("received %q, want %q", buf, "ping")
		}
		if _, err := conn.Write([]byte("pong")); err != nil {
			t.Fatalf("writing to accepted connection: %v", err)
		}
		_ = stream.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := io.ReadFull(stream, buf); err != nil {
			t.Fatalf("reading from peer stream: %v", err)
		}
		if string(buf) != "pong" {
			t.Fatalf("received %q, want %q", buf, "pong")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not accept the second peer while the first peer had no stream")
	}
}
