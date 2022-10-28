//go:build linux
// +build linux

package core

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"encoding/json"
	"strings"
	"strconv"

	"github.com/Arceliar/phony"
	sctp "github.com/vikulin/sctp"
)

type linkSCTP struct {
	phony.Inbox
	*links
	listener   *net.ListenConfig
	_listeners map[*Listener]context.CancelFunc
}

func (l *links) newLinkSCTP() *linkSCTP {
	lt := &linkSCTP{
		links: l,
		listener: &net.ListenConfig{
			KeepAlive: -1,
		},
		_listeners: map[*Listener]context.CancelFunc{},
	}
	return lt
}

func (l *linkSCTP) dial(url *url.URL, options linkOptions, sintf string) error {
	info := linkInfoFor("sctp", sintf, strings.SplitN(url.Host, "%", 2)[0])
	if l.links.isConnectedTo(info) {
		return nil
	}
	host, port, err := net.SplitHostPort(url.Host)
	if err != nil {
		return err
	}
	dst, err := net.ResolveIPAddr("ip", host)
	if err != nil {
		return err
	}
	raddress := l.getAddress(dst.String()+":"+port)
	var conn net.Conn
	laddress := l.getAddress("0.0.0.0:0")
	conn, err = sctp.NewSCTPConnection(laddress, laddress.AddressFamily, sctp.InitMsg{NumOstreams: 2, MaxInstreams: 2, MaxAttempts: 2, MaxInitTimeout: 5}, sctp.OneToOne, false)
	if err != nil {
		return err
	}
	err = conn.(*sctp.SCTPConn).Connect(raddress)
	//conn.(*sctp.SCTPConn).SetWriteBuffer(324288)
	//conn.(*sctp.SCTPConn).SetReadBuffer(324288)
	//wbuf, _ := conn.(*sctp.SCTPConn).GetWriteBuffer()
	//rbuf, _ := conn.(*sctp.SCTPConn).GetReadBuffer()

	//l.core.log.Printf("Read buffer %d", rbuf)
	//l.core.log.Printf("Write buffer %d", wbuf)
	conn.(*sctp.SCTPConn).SetEvents(sctp.SCTP_EVENT_DATA_IO)
	return l.handler(url.String(), info, conn, options, false, false)
}

func (l *linkSCTP) listen(url *url.URL, sintf string) (*Listener, error) {
	//_, cancel := context.WithCancel(l.core.ctx)
	hostport := url.Host
	if sintf != "" {
		if host, port, err := net.SplitHostPort(hostport); err == nil {
			hostport = fmt.Sprintf("[%s%%%s]:%s", host, sintf, port)
		}
	}
	addr := l.getAddress(url.Host)
	listener, err := sctp.NewSCTPListener(addr, sctp.InitMsg{NumOstreams: 2, MaxInstreams: 2, MaxAttempts: 2, MaxInitTimeout: 5}, sctp.OneToOne, false)

	if err != nil {
		//cancel()
		return nil, err
	}
	listener.SetEvents(sctp.SCTP_EVENT_DATA_IO)
	entry := &Listener{
		Listener: listener,
		closed:   make(chan struct{}),
	}
	//phony.Block(l, func() {
	//	l._listeners[entry] = cancel
	//})
	l.core.log.Printf("SCTP listener started on %s", listener.Addr())
	go func() {
		defer phony.Block(l, func() {
			delete(l._listeners, entry)
		})
		for {
			conn, err := listener.Accept()
			if err != nil {
				//cancel()
				break
			}
			addr := conn.RemoteAddr().(*sctp.SCTPAddr)
			ips, err := json.Marshal(addr.IPAddrs)
                        if err != nil {
                                break
                        }
			name := fmt.Sprintf("sctp://%s", ips)
			info := linkInfoFor("sctp", sintf, string(ips))
			//conn.(*sctp.SCTPConn).SetWriteBuffer(324288)
			//conn.(*sctp.SCTPConn).SetReadBuffer(324288)
			wbuf, _ := conn.(*sctp.SCTPConn).GetWriteBuffer()
			rbuf, _ := conn.(*sctp.SCTPConn).GetReadBuffer()

			l.core.log.Printf("Read buffer %d", rbuf)
		        l.core.log.Printf("Write buffer %d", wbuf)
			if err = l.handler(name, info, conn, linkOptions{}, true, addr.IP.IsLinkLocalUnicast()); err != nil {
				l.core.log.Errorln("Failed to create inbound link:", err)
			}
		}
		_ = listener.Close()
		close(entry.closed)
		l.core.log.Printf("SCTP listener stopped on %s", listener.Addr())
	}()
	return entry, nil
}

func (l *linkSCTP) handler(name string, info linkInfo, conn net.Conn, options linkOptions, incoming bool, force boo) error {
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
func (l *linkSCTP) getAddr() *net.TCPAddr {
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

//SCTP infrastructure
func (l *linkSCTP) getAddress(host string) *sctp.SCTPAddr {

	//sctp supports multihoming but current implementation reuires only one path
	ips := []net.IPAddr{}
	ip, port, err := net.SplitHostPort(host)
	if err != nil {
		panic(err)
	}
	for _, i := range strings.Split(ip, ",") {
		if a, err := net.ResolveIPAddr("ip", i); err == nil {
			fmt.Sprintf("Resolved address '%s' to %s", i, a)
			ips = append(ips, *a)
		} else {
			l.core.log.Errorln("Error resolving address '%s': %v", i, err)
		}
	}
	p, _ := strconv.Atoi(port)
	addr := &sctp.SCTPAddr{
		IPAddrs: ips,
		Port:    p,
	}
	return addr
}
