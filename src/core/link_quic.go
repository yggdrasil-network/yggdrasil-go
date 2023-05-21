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

type linkQUIC struct {
	phony.Inbox
	*links
	tlsconfig  *tls.Config
	quicconfig *quic.Config
}

type linkQUICStream struct {
	quic.Connection
	quic.Stream
}

type linkQUICListener struct {
	quic.EarlyListener
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

func (l *linkQUIC) dial(url *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	qc, err := quic.DialAddrEarly(url.Host, l.tlsconfig, l.quicconfig)
	if err != nil {
		fmt.Println("Dial error:", err)
		return nil, err
	}
	qs, err := qc.OpenStream()
	if err != nil {
		fmt.Println("Stream error:", err)
		return nil, err
	}
	return &linkQUICStream{
		Connection: qc,
		Stream:     qs,
	}, nil
}

func (l *linkQUIC) listen(ctx context.Context, url *url.URL, _ string) (net.Listener, error) {
	ql, err := quic.ListenAddrEarly(url.Host, l.tlsconfig, l.quicconfig)
	if err != nil {
		return nil, err
	}
	ch := make(chan *linkQUICStream)
	lql := &linkQUICListener{
		EarlyListener: ql,
		ch:            ch,
	}
	go func() {
		for {
			qc, err := ql.Accept(ctx)
			if err != nil {
				ql.Close()
				return
			}
			qs, err := qc.AcceptStream(ctx)
			if err != nil {
				ql.Close()
				return
			}
			ch <- &linkQUICStream{
				Connection: qc,
				Stream:     qs,
			}
		}
	}()
	return lql, nil
}
