package yggdrasil

// This sends packets to peers using TCP as a transport
// It's generally better tested than the UDP implementation
// Using it regularly is insane, but I find TCP easier to test/debug with it
// Updating and optimizing the UDP version is a higher priority

// TODO:
//  Something needs to make sure we're getting *valid* packets
//  Could be used to DoS (connect, give someone else's keys, spew garbage)
//  I guess the "peer" part should watch for link packets, disconnect?

// TCP connections start with a metadata exchange.
//  It involves exchanging version numbers and crypto keys
//  See version.go for version metadata format

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

const default_timeout = 6 * time.Second
const tcp_ping_interval = (default_timeout * 2 / 3)

// The TCP listener and information about active TCP connections, to avoid duplication.
type tcp struct {
	link        *link
	reconfigure chan chan error
	mutex       sync.Mutex // Protecting the below
	listeners   map[string]*TcpListener
	calls       map[string]struct{}
	conns       map[linkInfo](chan struct{})
}

// TcpListener is a stoppable TCP listener interface. These are typically
// returned from calls to the ListenTCP() function and are also used internally
// to represent listeners created by the "Listen" configuration option and for
// multicast interfaces.
type TcpListener struct {
	Listener net.Listener
	Stop     chan bool
}

// Wrapper function to set additional options for specific connection types.
func (t *tcp) setExtraOptions(c net.Conn) {
	switch sock := c.(type) {
	case *net.TCPConn:
		sock.SetNoDelay(true)
	// TODO something for socks5
	default:
	}
}

// Returns the address of the listener.
func (t *tcp) getAddr() *net.TCPAddr {
	// TODO: Fix this, because this will currently only give a single address
	// to multicast.go, which obviously is not great, but right now multicast.go
	// doesn't have the ability to send more than one address in a packet either
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for _, l := range t.listeners {
		return l.Listener.Addr().(*net.TCPAddr)
	}
	return nil
}

// Initializes the struct.
func (t *tcp) init(l *link) error {
	t.link = l
	t.reconfigure = make(chan chan error, 1)
	t.mutex.Lock()
	t.calls = make(map[string]struct{})
	t.conns = make(map[linkInfo](chan struct{}))
	t.listeners = make(map[string]*TcpListener)
	t.mutex.Unlock()

	go func() {
		for {
			e := <-t.reconfigure
			t.link.core.config.Mutex.RLock()
			added := util.Difference(t.link.core.config.Current.Listen, t.link.core.config.Previous.Listen)
			deleted := util.Difference(t.link.core.config.Previous.Listen, t.link.core.config.Current.Listen)
			t.link.core.config.Mutex.RUnlock()
			if len(added) > 0 || len(deleted) > 0 {
				for _, a := range added {
					if a[:6] != "tcp://" {
						continue
					}
					if _, err := t.listen(a[6:]); err != nil {
						e <- err
						continue
					}
				}
				for _, d := range deleted {
					if d[:6] != "tcp://" {
						continue
					}
					t.mutex.Lock()
					if listener, ok := t.listeners[d[6:]]; ok {
						t.mutex.Unlock()
						listener.Stop <- true
					} else {
						t.mutex.Unlock()
					}
				}
				e <- nil
			} else {
				e <- nil
			}
		}
	}()

	t.link.core.config.Mutex.RLock()
	defer t.link.core.config.Mutex.RUnlock()
	for _, listenaddr := range t.link.core.config.Current.Listen {
		if listenaddr[:6] != "tcp://" {
			continue
		}
		if _, err := t.listen(listenaddr[6:]); err != nil {
			return err
		}
	}

	return nil
}

func (t *tcp) listen(listenaddr string) (*TcpListener, error) {
	var err error

	ctx := context.Background()
	lc := net.ListenConfig{
		Control: t.tcpContext,
	}
	listener, err := lc.Listen(ctx, "tcp", listenaddr)
	if err == nil {
		l := TcpListener{
			Listener: listener,
			Stop:     make(chan bool),
		}
		go t.listener(&l, listenaddr)
		return &l, nil
	}

	return nil, err
}

// Runs the listener, which spawns off goroutines for incoming connections.
func (t *tcp) listener(l *TcpListener, listenaddr string) {
	if l == nil {
		return
	}
	// Track the listener so that we can find it again in future
	t.mutex.Lock()
	if _, isIn := t.listeners[listenaddr]; isIn {
		t.mutex.Unlock()
		l.Listener.Close()
		return
	} else {
		t.listeners[listenaddr] = l
		t.mutex.Unlock()
	}
	// And here we go!
	accepted := make(chan bool)
	defer func() {
		t.link.core.log.Infoln("Stopping TCP listener on:", l.Listener.Addr().String())
		l.Listener.Close()
		t.mutex.Lock()
		delete(t.listeners, listenaddr)
		t.mutex.Unlock()
	}()
	t.link.core.log.Infoln("Listening for TCP on:", l.Listener.Addr().String())
	for {
		var sock net.Conn
		var err error
		// Listen in a separate goroutine, as that way it does not block us from
		// receiving "stop" events
		go func() {
			sock, err = l.Listener.Accept()
			accepted <- true
		}()
		// Wait for either an accepted connection, or a message telling us to stop
		// the TCP listener
		select {
		case <-accepted:
			if err != nil {
				t.link.core.log.Errorln("Failed to accept connection:", err)
				return
			}
			go t.handler(sock, true, nil)
		case <-l.Stop:
			return
		}
	}
}

// Checks if we already are calling this address
func (t *tcp) isAlreadyCalling(saddr string) bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	_, isIn := t.calls[saddr]
	return isIn
}

// Checks if a connection already exists.
// If not, it adds it to the list of active outgoing calls (to block future attempts) and dials the address.
// If the dial is successful, it launches the handler.
// When finished, it removes the outgoing call, so reconnection attempts can be made later.
// This all happens in a separate goroutine that it spawns.
func (t *tcp) call(saddr string, options interface{}, sintf string) {
	go func() {
		callname := saddr
		if sintf != "" {
			callname = fmt.Sprintf("%s/%s", saddr, sintf)
		}
		if t.isAlreadyCalling(callname) {
			return
		}
		t.mutex.Lock()
		t.calls[callname] = struct{}{}
		t.mutex.Unlock()
		defer func() {
			// Block new calls for a little while, to mitigate livelock scenarios
			time.Sleep(default_timeout)
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
			t.mutex.Lock()
			delete(t.calls, callname)
			t.mutex.Unlock()
		}()
		var conn net.Conn
		var err error
		socksaddr, issocks := options.(string)
		if issocks {
			if sintf != "" {
				return
			}
			dialerdst, er := net.ResolveTCPAddr("tcp", socksaddr)
			if er != nil {
				return
			}
			var dialer proxy.Dialer
			dialer, err = proxy.SOCKS5("tcp", dialerdst.String(), nil, proxy.Direct)
			if err != nil {
				return
			}
			conn, err = dialer.Dial("tcp", saddr)
			if err != nil {
				return
			}
			conn = &wrappedConn{
				c: conn,
				raddr: &wrappedAddr{
					network: "tcp",
					addr:    saddr,
				},
			}
			t.handler(conn, false, dialerdst.String())
		} else {
			dst, err := net.ResolveTCPAddr("tcp", saddr)
			if err != nil {
				return
			}
			if dst.IP.IsLinkLocalUnicast() {
				dst.Zone = sintf
				if dst.Zone == "" {
					return
				}
			}
			dialer := net.Dialer{
				Control: t.tcpContext,
			}
			if sintf != "" {
				ief, err := net.InterfaceByName(sintf)
				if err != nil {
					return
				}
				if ief.Flags&net.FlagUp == 0 {
					return
				}
				addrs, err := ief.Addrs()
				if err == nil {
					for addrindex, addr := range addrs {
						src, _, err := net.ParseCIDR(addr.String())
						if err != nil {
							continue
						}
						if src.Equal(dst.IP) {
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
						return
					}
				}
			}
			conn, err = dialer.Dial("tcp", dst.String())
			if err != nil {
				t.link.core.log.Debugln("Failed to dial TCP:", err)
				return
			}
			t.handler(conn, false, nil)
		}
	}()
}

func (t *tcp) handler(sock net.Conn, incoming bool, options interface{}) {
	defer sock.Close()
	t.setExtraOptions(sock)
	stream := stream{}
	stream.init(sock)
	local, _, _ := net.SplitHostPort(sock.LocalAddr().String())
	remote, _, _ := net.SplitHostPort(sock.RemoteAddr().String())
	force := net.ParseIP(strings.Split(remote, "%")[0]).IsLinkLocalUnicast()
	var name string
	var proto string
	if socksaddr, issocks := options.(string); issocks {
		name = "socks://" + socksaddr + "/" + sock.RemoteAddr().String()
		proto = "socks"
	} else {
		name = "tcp://" + sock.RemoteAddr().String()
		proto = "tcp"
	}
	link, err := t.link.core.link.create(&stream, name, proto, local, remote, incoming, force)
	if err != nil {
		t.link.core.log.Println(err)
		panic(err)
	}
	t.link.core.log.Debugln("DEBUG: starting handler for", name)
	err = link.handler()
	t.link.core.log.Debugln("DEBUG: stopped handler for", name, err)
}
