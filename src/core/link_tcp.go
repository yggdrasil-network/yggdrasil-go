package core

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/Arceliar/phony"
)

type linkTCP struct {
	phony.Inbox
	*links
	listener   *net.ListenConfig
	_listeners map[*Listener]context.CancelFunc
}

func (l *links) newLinkTCP() *linkTCP {
	lt := &linkTCP{
		links: l,
		listener: &net.ListenConfig{
			KeepAlive: -1,
		},
		_listeners: map[*Listener]context.CancelFunc{},
	}
	lt.listener.Control = lt.tcpContext
	return lt
}

func (l *linkTCP) dial(url *url.URL, options linkOptions, sintf string) error {
	addr, err := net.ResolveTCPAddr("tcp", url.Host)
	if err != nil {
		return err
	}
	dialer, err := l.dialerFor(addr, sintf)
	if err != nil {
		return err
	}
	info := linkInfoFor("tcp", sintf, tcpIDFor(dialer.LocalAddr, addr))
	if l.links.isConnectedTo(info) {
		return nil
	}
	conn, err := dialer.DialContext(l.core.ctx, "tcp", addr.String())
	if err != nil {
		return err
	}
	uri := strings.TrimRight(strings.SplitN(url.String(), "?", 2)[0], "/")
	return l.handler(uri, info, conn, options, false, false)
}

func (l *linkTCP) listen(url *url.URL, sintf string) (*Listener, error) {
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
	entry := &Listener{
		Listener: listener,
		closed:   make(chan struct{}),
	}
	phony.Block(l, func() {
		l._listeners[entry] = cancel
	})
	l.core.log.Printf("TCP listener started on %s", listener.Addr())
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
			laddr := conn.LocalAddr().(*net.TCPAddr)
			raddr := conn.RemoteAddr().(*net.TCPAddr)
			name := fmt.Sprintf("tcp://%s", raddr)
			info := linkInfoFor("tcp", sintf, tcpIDFor(laddr, raddr))
			if err = l.handler(name, info, conn, linkOptionsForListener(url), true, raddr.IP.IsLinkLocalUnicast()); err != nil {
				l.core.log.Errorln("Failed to create inbound link:", err)
			}
		}
		_ = listener.Close()
		close(entry.closed)
		l.core.log.Printf("TCP listener stopped on %s", listener.Addr())
	}()
	return entry, nil
}

func (l *linkTCP) handler(name string, info linkInfo, conn net.Conn, options linkOptions, incoming, force bool) error {
	return l.links.create(
		conn,     // connection
		name,     // connection name
		info,     // connection info
		incoming, // not incoming
		force,    // not forced
		options,  // connection options
	)
}

// Returns the address of the listener.
func (l *linkTCP) getAddr() *net.TCPAddr {
	// TODO: Fix this, because this will currently only give a single address
	// to multicast.go, which obviously is not great, but right now multicast.go
	// doesn't have the ability to send more than one address in a packet either
	var addr *net.TCPAddr
	phony.Block(l, func() {
		for listener := range l._listeners {
			addr = listener.Addr().(*net.TCPAddr)
		}
	})
	return addr
}

func (l *linkTCP) dialerFor(dst *net.TCPAddr, sintf string) (*net.Dialer, error) {
	if dst.IP.IsLinkLocalUnicast() {
		if sintf != "" {
			dst.Zone = sintf
		}
		if dst.Zone == "" {
			return nil, fmt.Errorf("link-local address requires a zone")
		}
	}
	dialer := &net.Dialer{
		Timeout:   time.Second * 5,
		KeepAlive: -1,
		Control:   l.tcpContext,
	}
	if sintf != "" {
		dialer.Control = l.getControl(sintf)
		ief, err := net.InterfaceByName(sintf)
		if err != nil {
			return nil, fmt.Errorf("interface %q not found", sintf)
		}
		if ief.Flags&net.FlagUp == 0 {
			return nil, fmt.Errorf("interface %q is not up", sintf)
		}
		addrs, err := ief.Addrs()
		if err != nil {
			return nil, fmt.Errorf("interface %q addresses not available: %w", sintf, err)
		}
		for addrindex, addr := range addrs {
			src, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				continue
			}
			if !src.IsGlobalUnicast() && !src.IsLinkLocalUnicast() {
				continue
			}
			bothglobal := src.IsGlobalUnicast() == dst.IP.IsGlobalUnicast()
			bothlinklocal := src.IsLinkLocalUnicast() == dst.IP.IsLinkLocalUnicast()
			if !bothglobal && !bothlinklocal {
				continue
			}
			if (src.To4() != nil) != (dst.IP.To4() != nil) {
				continue
			}
			if bothglobal || bothlinklocal || addrindex == len(addrs)-1 {
				dialer.LocalAddr = &net.TCPAddr{
					IP:   src,
					Port: 0,
					Zone: sintf,
				}
				break
			}
		}
		if dialer.LocalAddr == nil {
			return nil, fmt.Errorf("no suitable source address found on interface %q", sintf)
		}
	}
	return dialer, nil
}

func tcpIDFor(local net.Addr, remoteAddr *net.TCPAddr) string {
	if localAddr, ok := local.(*net.TCPAddr); ok && localAddr.IP.Equal(remoteAddr.IP) {
		// Nodes running on the same host — include both the IP and port.
		return remoteAddr.String()
	}
	if remoteAddr.IP.IsLinkLocalUnicast() {
		// Nodes discovered via multicast — include the IP only.
		return remoteAddr.IP.String()
	}
	// Nodes connected remotely — include both the IP and port.
	return remoteAddr.String()
}
