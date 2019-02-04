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
)

const default_timeout = 6 * time.Second
const tcp_ping_interval = (default_timeout * 2 / 3)

// The TCP listener and information about active TCP connections, to avoid duplication.
type tcpInterface struct {
	core        *Core
	reconfigure chan chan error
	serv        net.Listener
	stop        chan bool
	timeout     time.Duration
	addr        string
	mutex       sync.Mutex // Protecting the below
	calls       map[string]struct{}
	conns       map[tcpInfo](chan struct{})
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
func (iface *tcpInterface) setExtraOptions(c net.Conn) {
	switch sock := c.(type) {
	case *net.TCPConn:
		sock.SetNoDelay(true)
	// TODO something for socks5
	default:
	}
}

// Returns the address of the listener.
func (iface *tcpInterface) getAddr() *net.TCPAddr {
	return iface.serv.Addr().(*net.TCPAddr)
}

// Attempts to initiate a connection to the provided address.
func (iface *tcpInterface) connect(addr string, intf string) {
	iface.call(addr, nil, intf)
}

// Attempst to initiate a connection to the provided address, viathe provided socks proxy address.
func (iface *tcpInterface) connectSOCKS(socksaddr, peeraddr string) {
	iface.call(peeraddr, &socksaddr, "")
}

// Initializes the struct.
func (iface *tcpInterface) init(core *Core) (err error) {
	iface.core = core
	iface.stop = make(chan bool, 1)
	iface.reconfigure = make(chan chan error, 1)
	go func() {
		for {
			e := <-iface.reconfigure
			iface.core.configMutex.RLock()
			updated := iface.core.config.Listen != iface.core.configOld.Listen
			iface.core.configMutex.RUnlock()
			if updated {
				iface.stop <- true
				iface.serv.Close()
				e <- iface.listen()
			} else {
				e <- nil
			}
		}
	}()

	return iface.listen()
}

func (iface *tcpInterface) listen() error {
	var err error

	iface.core.configMutex.RLock()
	iface.addr = iface.core.config.Listen
	iface.timeout = time.Duration(iface.core.config.ReadTimeout) * time.Millisecond
	iface.core.configMutex.RUnlock()

	if iface.timeout >= 0 && iface.timeout < default_timeout {
		iface.timeout = default_timeout
	}

	ctx := context.Background()
	lc := net.ListenConfig{
		Control: iface.tcpContext,
	}
	iface.serv, err = lc.Listen(ctx, "tcp", iface.addr)
	if err == nil {
		iface.mutex.Lock()
		iface.calls = make(map[string]struct{})
		iface.conns = make(map[tcpInfo](chan struct{}))
		iface.mutex.Unlock()
		go iface.listener()
		return nil
	}

	return err
}

// Runs the listener, which spawns off goroutines for incoming connections.
func (iface *tcpInterface) listener() {
	defer iface.serv.Close()
	iface.core.log.Infoln("Listening for TCP on:", iface.serv.Addr().String())
	for {
		sock, err := iface.serv.Accept()
		if err != nil {
			iface.core.log.Errorln("Failed to accept connection:", err)
			return
		}
		select {
		case <-iface.stop:
			iface.core.log.Errorln("Stopping listener")
			return
		default:
			if err != nil {
				panic(err)
			}
			go iface.handler(sock, true)
		}
	}
}

// Checks if we already have a connection to this node
func (iface *tcpInterface) isAlreadyConnected(info tcpInfo) bool {
	iface.mutex.Lock()
	defer iface.mutex.Unlock()
	_, isIn := iface.conns[info]
	return isIn
}

// Checks if we already are calling this address
func (iface *tcpInterface) isAlreadyCalling(saddr string) bool {
	iface.mutex.Lock()
	defer iface.mutex.Unlock()
	_, isIn := iface.calls[saddr]
	return isIn
}

// Checks if a connection already exists.
// If not, it adds it to the list of active outgoing calls (to block future attempts) and dials the address.
// If the dial is successful, it launches the handler.
// When finished, it removes the outgoing call, so reconnection attempts can be made later.
// This all happens in a separate goroutine that it spawns.
func (iface *tcpInterface) call(saddr string, socksaddr *string, sintf string) {
	go func() {
		callname := saddr
		if sintf != "" {
			callname = fmt.Sprintf("%s/%s", saddr, sintf)
		}
		if iface.isAlreadyCalling(callname) {
			return
		}
		iface.mutex.Lock()
		iface.calls[callname] = struct{}{}
		iface.mutex.Unlock()
		defer func() {
			// Block new calls for a little while, to mitigate livelock scenarios
			time.Sleep(default_timeout)
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
			iface.mutex.Lock()
			delete(iface.calls, callname)
			iface.mutex.Unlock()
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
				Control: iface.tcpContext,
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
		iface.handler(conn, false)
	}()
}

func (iface *tcpInterface) handler(sock net.Conn, incoming bool) {
	defer sock.Close()
	iface.setExtraOptions(sock)
	stream := stream{}
	stream.init(sock)
	local, _, _ := net.SplitHostPort(sock.LocalAddr().String())
	remote, _, _ := net.SplitHostPort(sock.RemoteAddr().String())
	remotelinklocal := net.ParseIP(remote).IsLinkLocalUnicast()
	name := "tcp://" + sock.RemoteAddr().String()
	link, err := iface.core.link.create(&stream, name, "tcp", local, remote, incoming, remotelinklocal)
	if err != nil {
		iface.core.log.Println(err)
		panic(err)
	}
	iface.core.log.Debugln("DEBUG: starting handler for", name)
	err = link.handler()
	iface.core.log.Debugln("DEBUG: stopped handler for", name, err)
}
