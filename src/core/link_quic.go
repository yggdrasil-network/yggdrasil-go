package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/Arceliar/phony"
	"github.com/quic-go/quic-go"
)

// quicStreamAcceptTimeout is how long an accepted QUIC connection is given
// to open the link stream before it is closed.
const quicStreamAcceptTimeout = 10 * time.Second

type linkQUIC struct {
	phony.Inbox
	*links
	tlsconfig  *tls.Config
	quicconfig *quic.Config
}

type linkQUICStream struct {
	*quic.Conn
	*quic.Stream
}

type linkQUICListener struct {
	*quic.Listener
	ch <-chan *linkQUICStream
}

func (l *linkQUICListener) Accept() (net.Conn, error) {
	qs := <-l.ch
	if qs == nil {
		return nil, context.Canceled
	}
	return qs, nil
}

func (l *links) newLinkQUIC() *linkQUIC {
	lt := &linkQUIC{
		links:     l,
		tlsconfig: l.core.config.tls.Clone(),
		quicconfig: &quic.Config{
			MaxIdleTimeout:  time.Minute,
			KeepAlivePeriod: time.Second * 20,
			TokenStore:      quic.NewLRUTokenStore(255, 255),
		},
	}
	return lt
}

func (l *linkQUIC) dial(ctx context.Context, url *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	tlsconfig := l.tlsconfig.Clone()
	return l.findSuitableIP(url, func(hostname string, ip net.IP, port int) (net.Conn, error) {
		tlsconfig.ServerName = hostname
		tlsconfig.MinVersion = tls.VersionTLS12
		tlsconfig.MaxVersion = tls.VersionTLS13
		hostport := net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port))
		qc, err := quic.DialAddr(ctx, hostport, l.tlsconfig, l.quicconfig)
		if err != nil {
			return nil, err
		}
		qs, err := qc.OpenStreamSync(ctx)
		if err != nil {
			return nil, err
		}
		return &linkQUICStream{
			Conn:   qc,
			Stream: qs,
		}, nil
	})
}

func (l *linkQUIC) listen(ctx context.Context, url *url.URL, _ string) (net.Listener, error) {
	ql, err := quic.ListenAddr(url.Host, l.tlsconfig, l.quicconfig)
	if err != nil {
		return nil, err
	}
	ch := make(chan *linkQUICStream)
	lql := &linkQUICListener{
		Listener: ql,
		ch:       ch,
	}
	go func() {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		for {
			qc, err := ql.Accept(ctx)
			switch err {
			case context.Canceled, context.DeadlineExceeded:
				ql.Close()
				fallthrough
			case quic.ErrServerClosed:
				return
			case nil:
				// Wait for the peer to open the link stream in the
				// background, so that a peer which completes the QUIC
				// handshake but never opens a stream cannot block the
				// listener from accepting other connections.
				go func(qc *quic.Conn) {
					sctx, scancel := context.WithTimeout(ctx, quicStreamAcceptTimeout)
					defer scancel()
					qs, err := qc.AcceptStream(sctx)
					if err != nil {
						_ = qc.CloseWithError(1, fmt.Sprintf("stream error: %s", err))
						return
					}
					select {
					case ch <- &linkQUICStream{
						Conn:   qc,
						Stream: qs,
					}:
					case <-ctx.Done():
						_ = qc.CloseWithError(1, "listener closed")
					}
				}(qc)
			}
		}
	}()
	return lql, nil
}
