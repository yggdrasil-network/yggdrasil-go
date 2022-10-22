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
	"strings"
	"time"

	"github.com/Arceliar/phony"
)

type linkTLS struct {
	phony.Inbox
	*links
	tcp        *linkTCP
	listener   *net.ListenConfig
	config     *tls.Config
	_listeners map[*Listener]context.CancelFunc
}

func (l *links) newLinkTLS(tcp *linkTCP) *linkTLS {
	lt := &linkTLS{
		links: l,
		tcp:   tcp,
		listener: &net.ListenConfig{
			Control:   tcp.tcpContext,
			KeepAlive: -1,
		},
		_listeners: map[*Listener]context.CancelFunc{},
	}
	var err error
	lt.config, err = lt.generateConfig()
	if err != nil {
		panic(err)
	}
	return lt
}

func (l *linkTLS) dial(url *url.URL, options linkOptions, sintf, sni string) error {
	addr, err := net.ResolveTCPAddr("tcp", url.Host)
	if err != nil {
		return err
	}
	dialer, err := l.tcp.dialerFor(addr, sintf)
	if err != nil {
		return err
	}
	info := linkInfoFor("tls", sintf, tcpIDFor(dialer.LocalAddr, addr))
	if l.links.isConnectedTo(info) {
		return nil
	}
	tlsconfig := l.config.Clone()
	tlsconfig.ServerName = sni
	tlsdialer := &tls.Dialer{
		NetDialer: dialer,
		Config:    tlsconfig,
	}
	conn, err := tlsdialer.DialContext(l.core.ctx, "tcp", addr.String())
	if err != nil {
		return err
	}
	uri := strings.TrimRight(strings.SplitN(url.String(), "?", 2)[0], "/")
	return l.handler(uri, info, conn, options, false, false)
}

func (l *linkTLS) listen(url *url.URL, sintf string) (*Listener, error) {
	ctx, cancel := context.WithCancel(l.core.ctx)
	hostport := url.Host
	if sintf != "" {
		if host, port, err := net.SplitHostPort(hostport); err == nil {
			hostport = fmt.Sprintf("[%s%%%s]:%s", host, sintf, port)
		}
	}
	listener, err := l.listener.Listen(ctx, "tcp", hostport)
	if err != nil {
		cancel()
		return nil, err
	}
	tlslistener := tls.NewListener(listener, l.config)
	entry := &Listener{
		Listener: tlslistener,
		closed:   make(chan struct{}),
	}
	phony.Block(l, func() {
		l._listeners[entry] = cancel
	})
	l.core.log.Printf("TLS listener started on %s", listener.Addr())
	go func() {
		defer phony.Block(l, func() {
			delete(l._listeners, entry)
		})
		for {
			conn, err := tlslistener.Accept()
			if err != nil {
				cancel()
				break
			}
			laddr := conn.LocalAddr().(*net.TCPAddr)
			raddr := conn.RemoteAddr().(*net.TCPAddr)
			name := fmt.Sprintf("tls://%s", raddr)
			info := linkInfoFor("tls", sintf, tcpIDFor(laddr, raddr))
			if err = l.handler(name, info, conn, linkOptionsForListener(url), true, raddr.IP.IsLinkLocalUnicast()); err != nil {
				l.core.log.Errorln("Failed to create inbound link:", err)
			}
		}
		_ = tlslistener.Close()
		close(entry.closed)
		l.core.log.Printf("TLS listener stopped on %s", listener.Addr())
	}()
	return entry, nil
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

func (l *linkTLS) handler(name string, info linkInfo, conn net.Conn, options linkOptions, incoming, force bool) error {
	return l.tcp.handler(name, info, conn, options, incoming, force)
}
