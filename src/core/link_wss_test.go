package core

import (
	"crypto/tls"
	"net/url"
	"testing"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// The WSS dial set the TLS ServerName only from the peering host and ignored
// the sni= peering option, so peers behind SNI-based virtual hosting could
// not be reached. This test captures the ClientHello SNI at a TLS listener
// and requires the sni= value to arrive.
func TestWSSDialSendsSNI(t *testing.T) {
	listenCfg := config.GenerateConfig()
	if err := listenCfg.GenerateSelfSignedCertificate(); err != nil {
		t.Fatal(err)
	}
	seen := make(chan string, 4)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{*listenCfg.Certificate},
		MinVersion:   tls.VersionTLS12,
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			seen <- hello.ServerName
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() // nolint:errcheck
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			// The WebSocket upgrade is not needed for this test: the TLS
			// handshake alone proves which SNI was sent. Drive the handshake
			// so GetConfigForClient fires, then drop the connection.
			if tc, ok := c.(*tls.Conn); ok {
				_ = tc.Handshake()
			}
			_ = c.Close()
		}
	}()

	dialCfg := config.GenerateConfig()
	if err := dialCfg.GenerateSelfSignedCertificate(); err != nil {
		t.Fatal(err)
	}
	node, err := New(dialCfg.Certificate, GetLoggerWithPrefix("", false))
	if err != nil {
		t.Fatal(err)
	}
	defer node.Stop()

	u, _ := url.Parse("wss://" + ln.Addr().String() + "?sni=front.example.org")
	if err := node.CallPeer(u, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case sni := <-seen:
		if sni != "front.example.org" {
			t.Fatalf("WSS ClientHello SNI = %q, want %q", sni, "front.example.org")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no TLS ClientHello arrived")
	}
}
