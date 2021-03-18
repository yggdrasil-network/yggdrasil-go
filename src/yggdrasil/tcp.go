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

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

const default_timeout = 6 * time.Second
const tcp_ping_interval = (default_timeout * 2 / 3)

// The TCP listener and information about active TCP connections, to avoid duplication.
type tcp struct {
	links     *links
	waitgroup sync.WaitGroup
	mutex     sync.Mutex // Protecting the below
	listeners map[string]*TcpListener
	calls     map[string]struct{}
	conns     map[linkInfo](chan struct{})
	tls       tcptls
}

// TcpListener is a stoppable TCP listener interface. These are typically
// returned from calls to the ListenTCP() function and are also used internally
// to represent listeners created by the "Listen" configuration option and for
// multicast interfaces.
type TcpListener struct {
	Listener net.Listener
	upgrade  *TcpUpgrade
	stop     chan struct{}
}

type TcpUpgrade struct {
	upgrade func(c net.Conn) (net.Conn, error)
	name    string
}

type tcpOptions struct {
	linkOptions
	upgrade        *TcpUpgrade
	socksProxyAddr string
	socksProxyAuth *proxy.Auth
	socksPeerAddr  string
}

func (l *TcpListener) Stop() {
	defer func() { recover() }()
	close(l.stop)
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
func (t *tcp) init(l *links) error {
	t.links = l
	t.tls.init(t)
	t.mutex.Lock()
	t.calls = make(map[string]struct{})
	t.conns = make(map[linkInfo](chan struct{}))
	t.listeners = make(map[string]*TcpListener)
	t.mutex.Unlock()

	t.links.core.config.Mutex.RLock()
	defer t.links.core.config.Mutex.RUnlock()
	for _, listenaddr := range t.links.core.config.Current.Listen {
		switch listenaddr[:6] {
		case "tcp://":
			if _, err := t.listen(listenaddr[6:], nil); err != nil {
				return err
			}
		case "tls://":
			if _, err := t.listen(listenaddr[6:], t.tls.forListener); err != nil {
				return err
			}
		default:
			t.links.core.log.Errorln("Failed to add listener: listener", listenaddr, "is not correctly formatted, ignoring")
		}
	}

	return nil
}

func (t *tcp) stop() error {
	t.mutex.Lock()
	for _, listener := range t.listeners {
		listener.Stop()
	}
	t.mutex.Unlock()
	t.waitgroup.Wait()
	return nil
}

func (t *tcp) reconfigure() {
	t.links.core.config.Mutex.RLock()
	added := util.Difference(t.links.core.config.Current.Listen, t.links.core.config.Previous.Listen)
	deleted := util.Difference(t.links.core.config.Previous.Listen, t.links.core.config.Current.Listen)
	t.links.core.config.Mutex.RUnlock()
	if len(added) > 0 || len(deleted) > 0 {
		for _, a := range added {
			switch a[:6] {
			case "tcp://":
				if _, err := t.listen(a[6:], nil); err != nil {
					t.links.core.log.Errorln("Error adding TCP", a[6:], "listener:", err)
				}
			case "tls://":
				if _, err := t.listen(a[6:], t.tls.forListener); err != nil {
					t.links.core.log.Errorln("Error adding TLS", a[6:], "listener:", err)
				}
			default:
				t.links.core.log.Errorln("Failed to add listener: listener", a, "is not correctly formatted, ignoring")
			}
		}
		for _, d := range deleted {
			if d[:6] != "tcp://" && d[:6] != "tls://" {
				t.links.core.log.Errorln("Failed to delete listener: listener", d, "is not correctly formatted, ignoring")
				continue
			}
			t.mutex.Lock()
			if listener, ok := t.listeners[d[6:]]; ok {
				t.mutex.Unlock()
				listener.Stop()
				t.links.core.log.Infoln("Stopped TCP listener:", d[6:])
			} else {
				t.mutex.Unlock()
			}
		}
	}
}

func (t *tcp) listen(listenaddr string, upgrade *TcpUpgrade) (*TcpListener, error) {
	var err error

	ctx := context.Background()
	lc := net.ListenConfig{
		Control: t.tcpContext,
	}
	listener, err := lc.Listen(ctx, "tcp", listenaddr)
	if err == nil {
		l := TcpListener{
			Listener: listener,
			upgrade:  upgrade,
			stop:     make(chan struct{}),
		}
		t.waitgroup.Add(1)
		go t.listener(&l, listenaddr)
		return &l, nil
	}

	return nil, err
}

// Runs the listener, which spawns off goroutines for incoming connections.
func (t *tcp) listener(l *TcpListener, listenaddr string) {
	defer t.waitgroup.Done()
	if l == nil {
		return
	}
	// Track the listener so that we can find it again in future
	t.mutex.Lock()
	if _, isIn := t.listeners[listenaddr]; isIn {
		t.mutex.Unlock()
		l.Listener.Close()
		return
	}
	t.listeners[listenaddr] = l
	t.mutex.Unlock()
	// And here we go!
	defer func() {
		t.links.core.log.Infoln("Stopping TCP listener on:", l.Listener.Addr().String())
		l.Listener.Close()
		t.mutex.Lock()
		delete(t.listeners, listenaddr)
		t.mutex.Unlock()
	}()
	t.links.core.log.Infoln("Listening for TCP on:", l.Listener.Addr().String())
	go func() {
		<-l.stop
		l.Listener.Close()
	}()
	defer l.Stop()
	for {
		sock, err := l.Listener.Accept()
		if err != nil {
			t.links.core.log.Errorln("Failed to accept connection:", err)
			select {
			case <-l.stop:
				return
			default:
			}
			time.Sleep(time.Second) // So we don't busy loop
			continue
		}
		t.waitgroup.Add(1)
		options := tcpOptions{
			upgrade: l.upgrade,
		}
		go t.handler(sock, true, options)
	}
}

// Checks if we already are calling this address
func (t *tcp) startCalling(saddr string) bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	_, isIn := t.calls[saddr]
	t.calls[saddr] = struct{}{}
	return !isIn
}

// Checks if a connection already exists.
// If not, it adds it to the list of active outgoing calls (to block future attempts) and dials the address.
// If the dial is successful, it launches the handler.
// When finished, it removes the outgoing call, so reconnection attempts can be made later.
// This all happens in a separate goroutine that it spawns.
func (t *tcp) call(saddr string, options tcpOptions, sintf string) {
	go func() {
		callname := saddr
		callproto := "TCP"
		if options.upgrade != nil {
			callproto = strings.ToUpper(options.upgrade.name)
		}
		if sintf != "" {
			callname = fmt.Sprintf("%s/%s/%s", callproto, saddr, sintf)
		}
		if !t.startCalling(callname) {
			return
		}
		defer func() {
			// Block new calls for a little while, to mitigate livelock scenarios
			rand.Seed(time.Now().UnixNano())
			delay := default_timeout + time.Duration(rand.Intn(10000))*time.Millisecond
			time.Sleep(delay)
			t.mutex.Lock()
			delete(t.calls, callname)
			t.mutex.Unlock()
		}()
		var conn net.Conn
		var err error
		if options.socksProxyAddr != "" {
			if sintf != "" {
				return
			}
			dialerdst, er := net.ResolveTCPAddr("tcp", options.socksProxyAddr)
			if er != nil {
				return
			}
			var dialer proxy.Dialer
			dialer, err = proxy.SOCKS5("tcp", dialerdst.String(), options.socksProxyAuth, proxy.Direct)
			if err != nil {
				return
			}
			conn, err = dialer.Dial("tcp", saddr)
			if err != nil {
				return
			}
			t.waitgroup.Add(1)
			options.socksPeerAddr = conn.RemoteAddr().String()
			if ch := t.handler(conn, false, options); ch != nil {
				<-ch
			}
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
				Timeout: time.Second * 5,
			}
			if sintf != "" {
				dialer.Control = t.getControl(sintf)
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
				t.links.core.log.Debugf("Failed to dial %s: %s", callproto, err)
				return
			}
			t.waitgroup.Add(1)
			if ch := t.handler(conn, false, options); ch != nil {
				<-ch
			}
		}
	}()
}

func (t *tcp) handler(sock net.Conn, incoming bool, options tcpOptions) chan struct{} {
	defer t.waitgroup.Done() // Happens after sock.close
	defer sock.Close()
	t.setExtraOptions(sock)
	var upgraded bool
	if options.upgrade != nil {
		var err error
		if sock, err = options.upgrade.upgrade(sock); err != nil {
			t.links.core.log.Errorln("TCP handler upgrade failed:", err)
			return nil
		}
		upgraded = true
	}
	stream := stream{}
	stream.init(sock)
	var name, proto, local, remote string
	if options.socksProxyAddr != "" {
		name = "socks://" + sock.RemoteAddr().String() + "/" + options.socksPeerAddr
		proto = "socks"
		local, _, _ = net.SplitHostPort(sock.LocalAddr().String())
		remote, _, _ = net.SplitHostPort(options.socksPeerAddr)
	} else {
		if upgraded {
			proto = options.upgrade.name
			name = proto + "://" + sock.RemoteAddr().String()
		} else {
			proto = "tcp"
			name = proto + "://" + sock.RemoteAddr().String()
		}
		local, _, _ = net.SplitHostPort(sock.LocalAddr().String())
		remote, _, _ = net.SplitHostPort(sock.RemoteAddr().String())
	}
	localIP := net.ParseIP(local)
	if localIP = localIP.To16(); localIP != nil {
		var laddr address.Address
		var lsubnet address.Subnet
		copy(laddr[:], localIP)
		copy(lsubnet[:], localIP)
		if laddr.IsValid() || lsubnet.IsValid() {
			// The local address is with the network address/prefix range
			// This would route ygg over ygg, which we don't want
			// FIXME ideally this check should happen outside of the core library
			//  Maybe dial/listen at the application level
			//  Then pass a net.Conn to the core library (after these kinds of checks are done)
			t.links.core.log.Debugln("Dropping ygg-tunneled connection", local, remote)
			return nil
		}
	}
	force := net.ParseIP(strings.Split(remote, "%")[0]).IsLinkLocalUnicast()
	link, err := t.links.create(&stream, name, proto, local, remote, incoming, force, options.linkOptions)
	if err != nil {
		t.links.core.log.Println(err)
		panic(err)
	}
	t.links.core.log.Debugln("DEBUG: starting handler for", name)
	ch, err := link.handler()
	t.links.core.log.Debugln("DEBUG: stopped handler for", name, err)
	return ch
}
