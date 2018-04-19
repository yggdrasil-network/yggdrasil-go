package yggdrasil

import (
	"errors"
	"golang.org/x/net/proxy"
	"net"
	"strings"
	"time"
	"yggdrasil/config"
)

type Dialer = proxy.Dialer

type muxedDialer struct {
	conf   config.NetConfig
	tor    Dialer
	direct Dialer
}

type wrappedConn struct {
	c     net.Conn
	raddr net.Addr
}

type wrappedAddr struct {
	network string
	addr    string
}

func (a *wrappedAddr) Network() string {
	return a.network
}

func (a *wrappedAddr) String() string {
	return a.addr
}

func (c *wrappedConn) Write(data []byte) (int, error) {
	return c.c.Write(data)
}

func (c *wrappedConn) Read(data []byte) (int, error) {
	return c.c.Read(data)
}

func (c *wrappedConn) SetDeadline(t time.Time) error {
	return c.c.SetDeadline(t)
}

func (c *wrappedConn) SetReadDeadline(t time.Time) error {
	return c.c.SetReadDeadline(t)
}

func (c *wrappedConn) SetWriteDeadline(t time.Time) error {
	return c.c.SetWriteDeadline(t)
}

func (c *wrappedConn) Close() error {
	return c.c.Close()
}

func (c *wrappedConn) LocalAddr() net.Addr {
	return c.c.LocalAddr()
}

func (c *wrappedConn) RemoteAddr() net.Addr {
	return c.raddr
}

func (d *muxedDialer) Dial(network, addr string) (net.Conn, error) {
	host, _, _ := net.SplitHostPort(addr)
	if d.conf.Tor.UseForAll || strings.HasSuffix(host, ".onion") {
		if !d.conf.Tor.Enabled {
			return nil, errors.New("tor not enabled")
		}
		c, err := d.tor.Dial(network, addr)
		if err == nil {
			c = &wrappedConn{
				c: c,
				raddr: &wrappedAddr{
					network: network,
					addr:    addr,
				},
			}
		}
		return c, err
	} else {
		return d.direct.Dial(network, addr)
	}
}

func NewDialer(c config.NetConfig) Dialer {
	tor, _ := proxy.SOCKS5("tcp", c.Tor.SocksAddr, nil, proxy.Direct)
	return &muxedDialer{
		conf:   c,
		tor:    tor,
		direct: proxy.Direct,
	}
}
