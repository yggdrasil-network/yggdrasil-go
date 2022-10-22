package core

import (
	"context"
	"net"
	"net/url"
	"time"

	"github.com/Arceliar/phony"
)

type linkUNIX struct {
	phony.Inbox
	*links
	dialer     *net.Dialer
	listener   *net.ListenConfig
	_listeners map[*Listener]context.CancelFunc
}

func (l *links) newLinkUNIX() *linkUNIX {
	lt := &linkUNIX{
		links: l,
		dialer: &net.Dialer{
			Timeout:   time.Second * 5,
			KeepAlive: -1,
		},
		listener: &net.ListenConfig{
			KeepAlive: -1,
		},
		_listeners: map[*Listener]context.CancelFunc{},
	}
	return lt
}

func (l *linkUNIX) dial(url *url.URL, options linkOptions, _ string) error {
	info := linkInfoFor("unix", "", url.Path)
	if l.links.isConnectedTo(info) {
		return nil
	}
	addr, err := net.ResolveUnixAddr("unix", url.Path)
	if err != nil {
		return err
	}
	conn, err := l.dialer.DialContext(l.core.ctx, "unix", addr.String())
	if err != nil {
		return err
	}
	return l.handler(url.String(), info, conn, options, false)
}

func (l *linkUNIX) listen(url *url.URL, _ string) (*Listener, error) {
	ctx, cancel := context.WithCancel(l.core.ctx)
	listener, err := l.listener.Listen(ctx, "unix", url.Path)
	if err != nil {
		cancel()
		return nil, err
	}
	entry := &Listener{
		Listener: listener,
		closed:   make(chan struct{}),
	}
	phony.Block(l, func() {
		l._listeners[entry] = cancel
	})
	l.core.log.Printf("UNIX listener started on %s", listener.Addr())
	go func() {
		defer phony.Block(l, func() {
			delete(l._listeners, entry)
		})
		for {
			conn, err := listener.Accept()
			if err != nil {
				cancel()
				break
			}
			info := linkInfoFor("unix", "", url.String())
			if err = l.handler(url.String(), info, conn, linkOptionsForListener(url), true); err != nil {
				l.core.log.Errorln("Failed to create inbound link:", err)
			}
		}
		_ = listener.Close()
		close(entry.closed)
		l.core.log.Printf("UNIX listener stopped on %s", listener.Addr())
	}()
	return entry, nil
}

func (l *linkUNIX) handler(name string, info linkInfo, conn net.Conn, options linkOptions, incoming bool) error {
	return l.links.create(
		conn,     // connection
		name,     // connection name
		info,     // connection info
		incoming, // not incoming
		false,    // not forced
		options,  // connection options
	)
}
