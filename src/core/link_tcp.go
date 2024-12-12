package core

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/Arceliar/phony"
)

type linkTCP struct {
	phony.Inbox
	*links
	listenconfig *net.ListenConfig
}

func (l *links) newLinkTCP() *linkTCP {
	lt := &linkTCP{
		links: l,
		listenconfig: &net.ListenConfig{
			KeepAlive: -1,
		},
	}
	lt.listenconfig.Control = lt.tcpContext
	return lt
}

func (l *linkTCP) dial(ctx context.Context, url *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	return l.links.findSuitableIP(url, func(hostname string, ip net.IP, port int) (net.Conn, error) {
		addr := &net.TCPAddr{
			IP:   ip,
			Port: port,
		}
		dialer, err := l.tcp.dialerFor(addr, info.sintf)
		if err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, "tcp", addr.String())
	})
}

func (l *linkTCP) listen(ctx context.Context, url *url.URL, sintf string) (net.Listener, error) {
	hostport := url.Host
	if sintf != "" {
		if host, port, err := net.SplitHostPort(hostport); err == nil {
			hostport = fmt.Sprintf("[%s%%%s]:%s", host, sintf, port)
		}
	}
	return l.listenconfig.Listen(ctx, "tcp", hostport)
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
