package core

import (
	"context"
	"crypto/tls"
	"net/url"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// The QUIC dial used to pass the base TLS config to quic.DialAddr instead of
// the prepared per-host copy, so no SNI was sent and the sni= peering option
// was ignored. This test captures the ClientHello at a QUIC listener and
// requires the sni= value to arrive as the SNI.
func TestQUICDialSendsSNI(t *testing.T) {
	listenCfg := config.GenerateConfig()
	if err := listenCfg.GenerateSelfSignedCertificate(); err != nil {
		t.Fatal(err)
	}
	seen := make(chan string, 4)
	ln, err := quic.ListenAddr("127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{*listenCfg.Certificate},
		MinVersion:   tls.VersionTLS13,
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			seen <- hello.ServerName
			return nil, nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() // nolint:errcheck
	go func() {
		for {
			c, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			_ = c.CloseWithError(0, "")
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

	u, _ := url.Parse("quic://" + ln.Addr().String() + "?sni=front.example.org")
	if err := node.CallPeer(u, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case sni := <-seen:
		if sni != "front.example.org" {
			t.Fatalf("QUIC ClientHello SNI = %q, want %q", sni, "front.example.org")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no QUIC ClientHello arrived")
	}
}
