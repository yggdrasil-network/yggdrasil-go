package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"time"

	"github.com/Arceliar/phony"
)

type linkTLS struct {
	phony.Inbox
	*links
	tcp        *linkTCP
	listener   *net.ListenConfig
	config     *tls.Config
	_listeners map[net.Listener]context.CancelFunc
}

func (l *links) newLinkTLS(tcp *linkTCP) *linkTLS {
	lt := &linkTLS{
		links: l,
		tcp:   tcp,
		listener: &net.ListenConfig{
			Control:   tcp.tcpContext,
			KeepAlive: -1,
		},
		_listeners: map[net.Listener]context.CancelFunc{},
	}
	var err error
	lt.config, err = lt.generateConfig()
	if err != nil {
		panic(err)
	}
	return lt
}

func (l *linkTLS) dial(url *url.URL, options tcpOptions, sintf string) (*link, error) {
	addr, err := net.ResolveTCPAddr("tcp", url.Host)
	if err != nil {
		return nil, err
	}
	addr.Zone = sintf
	dialer, err := l.tcp.dialerFor(addr.String(), sintf)
	if err != nil {
		return nil, err
	}
	tlsdialer := &tls.Dialer{
		NetDialer: dialer,
		Config:    l.config,
	}
	conn, err := tlsdialer.DialContext(l.core.ctx, "tcp", addr.String())
	if err != nil {
		return nil, err
	}
	if _, err = l.handler(conn, options, false); err != nil {
		l.core.log.Errorln("Failed to create outbound link:", err)
	}
	return nil, err
}

func (l *linkTLS) listen(url *url.URL, sintf string) (*Listener, error) {
	ctx, cancel := context.WithCancel(l.core.ctx)
	hostport := url.Host
	if sintf != "" {
		host, port, err := net.SplitHostPort(hostport)
		if err == nil {
			hostport = fmt.Sprintf("[%s%%%s]:%s", host, sintf, port)
		}
	}
	listener, err := l.listener.Listen(ctx, "tcp", hostport)
	if err != nil {
		cancel()
		return nil, err
	}
	tlslistener := tls.NewListener(listener, l.config)
	phony.Block(l, func() {
		l._listeners[tlslistener] = cancel
	})
	go func() {
		defer phony.Block(l, func() {
			delete(l._listeners, tlslistener)
		})
		for {
			conn, err := tlslistener.Accept()
			if err != nil {
				cancel()
				return
			}
			if _, err := l.handler(conn, tcpOptions{}, true); err != nil {
				l.core.log.Errorln("Failed to create inbound link:", err)
			}
		}
	}()
	return &Listener{
		Listener: tlslistener,
		Close:    cancel,
	}, nil
}

func (l *linkTLS) generateConfig() (*tls.Config, error) {
	certBuf := &bytes.Buffer{}

	// TODO: because NotAfter is finite, we should add some mechanism to
	// regenerate the certificate and restart the listeners periodically
	// for nodes with very high uptimes. Perhaps regenerate certs and restart
	// listeners every few months or so.
	cert := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: hex.EncodeToString(l.links.core.public[:]),
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour * 24 * 365),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certbytes, err := x509.CreateCertificate(rand.Reader, &cert, &cert, l.links.core.public, l.links.core.secret)
	if err != nil {
		return nil, err
	}

	if err := pem.Encode(certBuf, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certbytes,
	}); err != nil {
		return nil, err
	}

	rootCAs := x509.NewCertPool()
	rootCAs.AppendCertsFromPEM(certbytes)

	return &tls.Config{
		RootCAs: rootCAs,
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{certbytes},
				PrivateKey:  l.links.core.secret,
			},
		},
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}, nil
}

func (l *linkTLS) handler(conn net.Conn, options tcpOptions, incoming bool) (*link, error) {
	return l.tcp.handler("TLS", conn, options, incoming)
}
