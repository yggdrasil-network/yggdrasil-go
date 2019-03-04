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
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

const default_timeout = 6 * time.Second
const tcp_ping_interval = (default_timeout * 2 / 3)

// The TCP listener and information about active TCP connections, to avoid duplication.
type tcp struct {
	link          *link
	reconfigure   chan chan error
	mutex         sync.Mutex // Protecting the below
	listeners     map[string]net.Listener
	listenerstops map[string]chan bool
	calls         map[string]struct{}
	conns         map[tcpInfo](chan struct{})
}

// This is used as the key to a map that tracks existing connections, to prevent multiple connections to the same keys and local/remote address pair from occuring.
// Different address combinations are allowed, so multi-homing is still technically possible (but not necessarily advisable).
type tcpInfo struct {
	box        crypto.BoxPubKey
	sig        crypto.SigPubKey
	localAddr  string
	remoteAddr string
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
	for _, listener := range t.listeners {
		return listener.Addr().(*net.TCPAddr)
	}
	t.mutex.Unlock()
	return nil
}

// Attempts to initiate a connection to the provided address.
func (t *tcp) connect(addr string, intf string) {
	t.call(addr, nil, intf)
}

// Attempst to initiate a connection to the provided address, viathe provided socks proxy address.
func (t *tcp) connectSOCKS(socksaddr, peeraddr string) {
	t.call(peeraddr, &socksaddr, "")
}

// Initializes the struct.
func (t *tcp) init(l *link) error {
	t.link = l
	t.reconfigure = make(chan chan error, 1)
	t.mutex.Lock()
	t.calls = make(map[string]struct{})
	t.conns = make(map[tcpInfo](chan struct{}))
	t.listeners = make(map[string]net.Listener)
	t.listenerstops = make(map[string]chan bool)
	t.mutex.Unlock()

	go func() {
		for {
			e := <-t.reconfigure
			t.link.core.configMutex.RLock()
			added := util.Difference(t.link.core.config.Listen, t.link.core.configOld.Listen)
			deleted := util.Difference(t.link.core.configOld.Listen, t.link.core.config.Listen)
			t.link.core.configMutex.RUnlock()
			if len(added) > 0 || len(deleted) > 0 {
				for _, add := range added {
					if add[:6] != "tcp://" {
						continue
					}
					if err := t.listen(add[6:]); err != nil {
						e <- err
						continue
					}
				}
				for _, delete := range deleted {
					t.link.core.log.Warnln("Removing listener", delete, "not currently implemented")
					/*t.mutex.Lock()
					if listener, ok := t.listeners[delete]; ok {
						listener.Close()
					}
					if listener, ok := t.listenerstops[delete]; ok {
						listener <- true
					}
					t.mutex.Unlock()*/
				}
				e <- nil
			} else {
				e <- nil
			}
		}
	}()

	t.link.core.configMutex.RLock()
	defer t.link.core.configMutex.RUnlock()
	for _, listenaddr := range t.link.core.config.Listen {
		if listenaddr[:6] != "tcp://" {
			continue
		}
		if err := t.listen(listenaddr[6:]); err != nil {
			return err
		}
	}

	return nil
}

func (t *tcp) listen(listenaddr string) error {
	var err error

	ctx := context.Background()
	lc := net.ListenConfig{
		Control: t.tcpContext,
	}
	listener, err := lc.Listen(ctx, "tcp", listenaddr)
	if err == nil {
		t.mutex.Lock()
		t.listeners[listenaddr] = listener
		t.listenerstops[listenaddr] = make(chan bool, 1)
		t.mutex.Unlock()
		go t.listener(listenaddr)
		return nil
	}

	return err
}

// Runs the listener, which spawns off goroutines for incoming connections.
func (t *tcp) listener(listenaddr string) {
	t.mutex.Lock()
	listener, ok := t.listeners[listenaddr]
	listenerstop, ok2 := t.listenerstops[listenaddr]
	t.mutex.Unlock()
	if !ok || !ok2 {
		t.link.core.log.Errorln("Tried to start TCP listener for", listenaddr, "which doesn't exist")
		return
	}
	reallistenaddr := listener.Addr().String()
	defer listener.Close()
	t.link.core.log.Infoln("Listening for TCP on:", reallistenaddr)
	for {
		var sock net.Conn
		var err error
		accepted := make(chan bool)
		go func() {
			sock, err = listener.Accept()
			accepted <- true
		}()
		select {
		case <-accepted:
			if err != nil {
				t.link.core.log.Errorln("Failed to accept connection:", err)
				return
			}
		case <-listenerstop:
			t.link.core.log.Errorln("Stopping TCP listener on:", reallistenaddr)
			return
		default:
			if err != nil {
				panic(err)
			}
			go t.handler(sock, true)
		}
	}
}

// Checks if we already have a connection to this node
func (t *tcp) isAlreadyConnected(info tcpInfo) bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	_, isIn := t.conns[info]
	return isIn
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
func (t *tcp) call(saddr string, socksaddr *string, sintf string) {
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
		if socksaddr != nil {
			if sintf != "" {
				return
			}
			var dialer proxy.Dialer
			dialer, err = proxy.SOCKS5("tcp", *socksaddr, nil, proxy.Direct)
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
		} else {
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
					dst, err := net.ResolveTCPAddr("tcp", saddr)
					if err != nil {
						return
					}
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

			conn, err = dialer.Dial("tcp", saddr)
			if err != nil {
				return
			}
		}
		t.handler(conn, false)
	}()
}

func (t *tcp) handler(sock net.Conn, incoming bool) {
	defer sock.Close()
	t.setExtraOptions(sock)
	stream := stream{}
	stream.init(sock)
	local, _, _ := net.SplitHostPort(sock.LocalAddr().String())
	remote, _, _ := net.SplitHostPort(sock.RemoteAddr().String())
	remotelinklocal := net.ParseIP(remote).IsLinkLocalUnicast()
	name := "tcp://" + sock.RemoteAddr().String()
	link, err := t.link.core.link.create(&stream, name, "tcp", local, remote, incoming, remotelinklocal)
	if err != nil {
		t.link.core.log.Println(err)
		panic(err)
	}
	t.link.core.log.Debugln("DEBUG: starting handler for", name)
	err = link.handler()
	t.link.core.log.Debugln("DEBUG: stopped handler for", name, err)
}
