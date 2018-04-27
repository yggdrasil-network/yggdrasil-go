package yggdrasil

import (
	"net"
	"time"
)

// wrappedConn implements net.Conn
type wrappedConn struct {
	c     net.Conn
	raddr net.Addr
}

// wrappedAddr implements net.Addr
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
