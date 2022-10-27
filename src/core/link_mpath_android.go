//go:build android
// +build android

package core

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"net/netip"
	"strings"
	"github.com/getlantern/multipath"

	"github.com/Arceliar/phony"
)

type linkMPATH struct {
	phony.Inbox
	*links
	listener   *net.ListenConfig
	_listeners map[*Listener]context.CancelFunc
}

func (l *links) newLinkMPATH() *linkMPATH {
	lt := &linkMPATH{
		links: l,
		listener: &net.ListenConfig{
			KeepAlive: -1,
		},
		_listeners: map[*Listener]context.CancelFunc{},
	}
	lt.listener.Control = lt.tcpContext
	return lt
}

func (l *linkMPATH) dial(url *url.URL, options linkOptions, sintf string) error {
	info := linkInfoFor("mpath", sintf, strings.SplitN(url.Host, "%", 2)[0])
	if l.links.isConnectedTo(info) {
		return fmt.Errorf("duplicate connection attempt")
	}
	conn, err := l.connFor(url, sintf)
	if err != nil {
		return err
	}
	return l.handler(url.String(), info, conn, options, false)
}

func (l *linkMPATH) listen(url *url.URL, sintf string) (*Listener, error) {
	hostport := url.Host
	if sintf != "" {
		if host, port, err := net.SplitHostPort(hostport); err == nil {
			hostport = fmt.Sprintf("[%s%%%s]:%s", host, sintf, port)
		}
	}
	_, cancel := context.WithCancel(l.core.ctx)
	listener, err := l.listenFor(url, sintf)
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
	l.core.log.Printf("Multipath listener started on %s", listener.Addr())
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
			addr := conn.RemoteAddr().(*net.TCPAddr)
			name := fmt.Sprintf("mpath://%s", addr)
			info := linkInfoFor("mpath", sintf, strings.SplitN(addr.IP.String(), "%", 2)[0])
			if err = l.handler(name, info, conn, linkOptions{}, true); err != nil {
				l.core.log.Errorln("Failed to create inbound link:", err)
			}
		}
		_ = listener.Close()
		close(entry.closed)
		l.core.log.Printf("Multipath listener stopped on %s", listener.Addr())
	}()
	return entry, nil
}

func (l *linkMPATH) handler(name string, info linkInfo, conn net.Conn, options linkOptions, incoming bool) error {
	return l.links.create(
		conn,     // connection
		name,     // connection name
		info,     // connection info
		incoming, // not incoming
		false,    // not forced
		options,  // connection options
	)
}

// Returns the address of the listener.
func (l *linkMPATH) getAddr() *net.TCPAddr {
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


func (l *linkMPATH) connFor(url *url.URL, sinterfaces string) (net.Conn, error) {
	//Peer url has following format: mpath://host-1:port-1/host-2:port-2.../host-n:port-n
	hosts := strings.Split(url.String(), "/")[2:]
	remoteTargets := make([]net.Addr, 0)
	for _, host := range hosts {
		dst, err := net.ResolveTCPAddr("tcp", host)
		if err != nil {
			l.core.log.Errorln("could not resolve host ", dst.String())
			continue
		}
		if dst.IP.IsLinkLocalUnicast() {
			dst.Zone = sinterfaces
			if dst.Zone == "" {
				l.core.log.Errorln("link-local address requires a zone in ", dst.String())
				continue
			}
		}
		remoteTargets = append(remoteTargets, dst)
	}

	if len(remoteTargets) == 0 {
		return nil, fmt.Errorf("no valid target hosts given")
	}

	dialers := make([]multipath.Dialer, 0)
	trackers := make([]multipath.StatsTracker, 0)
	if sinterfaces != "" {
		sintfarray := strings.Split(sinterfaces, ",")
		for _, dst := range remoteTargets {
			for _, sintf := range sintfarray {
				addr, err := netip.ParseAddr(sintf)
				if err != nil {
					l.core.log.Errorln("interface %s address incorrect: %w", sintf, err)
					continue
				}
				src := net.ParseIP(addr.WithZone("").String())
				
				dstIp := dst.(*net.TCPAddr).IP

				if !src.IsGlobalUnicast() && !src.IsLinkLocalUnicast() {
					continue
				}
				bothglobal := src.IsGlobalUnicast() == dstIp.IsGlobalUnicast()
				bothlinklocal := src.IsLinkLocalUnicast() == dstIp.IsLinkLocalUnicast()
				if !bothglobal && !bothlinklocal {
					continue
				}
				if (src.To4() != nil) != (dstIp.To4() != nil) {
					continue
				}
				if bothglobal || bothlinklocal {
					td := newOutboundDialer(src, dst)
					dialers = append(dialers, td)
					trackers = append(trackers, multipath.NullTracker{})
					l.core.log.Printf("added outbound dialer for %s->%s", src.String(), dst.String())
				}
				
			}
		}
	} else {
		star := net.ParseIP("0.0.0.0")
		for _, dst := range remoteTargets {
			td := newOutboundDialer(star, dst)
			dialers = append(dialers, td)
			trackers = append(trackers, multipath.NullTracker{})
			l.core.log.Printf("added outbound dialer for %s", dst.String())
		}
	}
	if len(dialers) == 0 {
		return nil, fmt.Errorf("no suitable source address found on interface %q", sinterfaces)
	}
	dialer := multipath.NewDialer("mpath", dialers)
	//conn, err := dialer.DialContext(l.core.ctx, "tcp", remoteTargets[0].String())
	conn, err := dialer.DialContext(l.core.ctx)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (l *linkMPATH) listenFor(url *url.URL, sintf string) (net.Listener, error) {
	//Public node url has following format: mpath://ip-1:port-1/ip-2:port-2.../ip-n:port-n
	hosts := strings.Split(url.String(), "/")[2:]
	localTargets := make([]string, 0)
	for _, host := range hosts {
		dst, err := net.ResolveTCPAddr("tcp", host)
		if err != nil {
			l.core.log.Errorln("could not resolve host ", dst.String())
			continue
		}
		if dst.IP.IsLinkLocalUnicast() {
			dst.Zone = sintf
			if dst.Zone == "" {
				l.core.log.Errorln("link-local address requires a zone in ", dst.String())
				continue
			}
		}
		localTargets = append(localTargets, host)
	}

	if len(localTargets) == 0 {
		return nil, fmt.Errorf("no valid target hosts given")
	}

	listeners := make([]net.Listener, 0)
	trackers := make([]multipath.StatsTracker, 0)
	for _, lT := range localTargets {
		l, err := l.listener.Listen(l.core.ctx, "tcp", lT)
		if err != nil {
			continue
		}
		listeners = append(listeners, l)
		trackers = append(trackers, multipath.NullTracker{})
	}
	listener := multipath.NewListener(listeners, trackers)

	return listener, nil
}

type targetedDailer struct {
	localDialer net.Dialer
	remoteAddr  net.Addr
}

func newOutboundDialer(inputLocalAddr net.IP, inputRemoteAddr net.Addr) *targetedDailer {
	td := &targetedDailer{
		localDialer: net.Dialer{
			LocalAddr: &net.TCPAddr{
				IP:   inputLocalAddr,
				Port: 0,
			},
		},
		remoteAddr: inputRemoteAddr,
	}
	return td
}

func (td *targetedDailer) DialContext(ctx context.Context) (net.Conn, error) {
	conn, err := td.localDialer.DialContext(ctx, "tcp", td.remoteAddr.String())
	if err != nil {
		//l.core.log.Errorln("failed to dial to %v: %v", td.remoteAddr.String(), err)
		return nil, err
	}
	//l.core.log.Printf("Dialed to %v->%v", conn.LocalAddr(), td.remoteAddr.String())

	return conn, err
}

func (td *targetedDailer) Label() string {
	return fmt.Sprintf("%s|%s", td.localDialer.LocalAddr, td.remoteAddr)
}
