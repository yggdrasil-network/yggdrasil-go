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
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
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
	iface.core.log.Println("Listening for TCP on:", iface.serv.Addr().String())
	for {
		sock, err := iface.serv.Accept()
		if err != nil {
			iface.core.log.Println("Failed to accept connection:", err)
			return
		}
		select {
		case <-iface.stop:
			iface.core.log.Println("Stopping listener")
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
		if iface.isAlreadyCalling(saddr) {
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
	stream.init(sock, nil)
	local, _, _ := net.SplitHostPort(sock.LocalAddr().String())
	remote, _, _ := net.SplitHostPort(sock.RemoteAddr().String())
	name := "tcp://" + sock.RemoteAddr().String()
	link, err := iface.core.link.create(&stream, name, "tcp", local, remote)
	if err != nil {
		iface.core.log.Println(err)
		panic(err)
	}
	iface.core.log.Println("DEBUG: starting handler for", name)
	link.handler()
	iface.core.log.Println("DEBUG: stopped handler for", name)
}

// This exchanges/checks connection metadata, sets up the peer struct, sets up the writer goroutine, and then runs the reader within the current goroutine.
// It defers a bunch of cleanup stuff to tear down all of these things when the reader exists (e.g. due to a closed connection or a timeout).
func (iface *tcpInterface) handler_old(sock net.Conn, incoming bool) {
	defer sock.Close()
	iface.setExtraOptions(sock)
	// Get our keys
	myLinkPub, myLinkPriv := crypto.NewBoxKeys() // ephemeral link keys
	meta := version_getBaseMetadata()
	meta.box = iface.core.boxPub
	meta.sig = iface.core.sigPub
	meta.link = *myLinkPub
	metaBytes := meta.encode()
	_, err := sock.Write(metaBytes)
	if err != nil {
		return
	}
	if iface.timeout > 0 {
		sock.SetReadDeadline(time.Now().Add(iface.timeout))
	}
	_, err = sock.Read(metaBytes)
	if err != nil {
		return
	}
	meta = version_metadata{} // Reset to zero value
	if !meta.decode(metaBytes) || !meta.check() {
		// Failed to decode and check the metadata
		// If it's a version mismatch issue, then print an error message
		base := version_getBaseMetadata()
		if meta.meta == base.meta {
			if meta.ver > base.ver {
				iface.core.log.Println("Failed to connect to node:", sock.RemoteAddr().String(), "version:", meta.ver)
			} else if meta.ver == base.ver && meta.minorVer > base.minorVer {
				iface.core.log.Println("Failed to connect to node:", sock.RemoteAddr().String(), "version:", fmt.Sprintf("%d.%d", meta.ver, meta.minorVer))
			}
		}
		// TODO? Block forever to prevent future connection attempts? suppress future messages about the same node?
		return
	}
	remoteAddr, _, e1 := net.SplitHostPort(sock.RemoteAddr().String())
	localAddr, _, e2 := net.SplitHostPort(sock.LocalAddr().String())
	if e1 != nil || e2 != nil {
		return
	}
	info := tcpInfo{ // used as a map key, so don't include ephemeral link key
		box:        meta.box,
		sig:        meta.sig,
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
	}
	if iface.isAlreadyConnected(info) {
		return
	}
	// Quit the parent call if this is a connection to ourself
	equiv := func(k1, k2 []byte) bool {
		for idx := range k1 {
			if k1[idx] != k2[idx] {
				return false
			}
		}
		return true
	}
	if equiv(meta.box[:], iface.core.boxPub[:]) {
		return
	}
	if equiv(meta.sig[:], iface.core.sigPub[:]) {
		return
	}
	// Check if we're authorized to connect to this key / IP
	if incoming && !iface.core.peers.isAllowedEncryptionPublicKey(&meta.box) {
		// Allow unauthorized peers if they're link-local
		raddrStr, _, _ := net.SplitHostPort(sock.RemoteAddr().String())
		raddr := net.ParseIP(raddrStr)
		if !raddr.IsLinkLocalUnicast() {
			return
		}
	}
	// Check if we already have a connection to this node, close and block if yes
	iface.mutex.Lock()
	/*if blockChan, isIn := iface.conns[info]; isIn {
		iface.mutex.Unlock()
		sock.Close()
		<-blockChan
		return
	}*/
	blockChan := make(chan struct{})
	iface.conns[info] = blockChan
	iface.mutex.Unlock()
	defer func() {
		iface.mutex.Lock()
		delete(iface.conns, info)
		iface.mutex.Unlock()
		close(blockChan)
	}()
	// Note that multiple connections to the same node are allowed
	//  E.g. over different interfaces
	p := iface.core.peers.newPeer(&meta.box, &meta.sig, crypto.GetSharedKey(myLinkPriv, &meta.link), sock.RemoteAddr().String())
	p.linkOut = make(chan []byte, 1)
	out := make(chan []byte, 1)
	defer close(out)
	go func() {
		// This goroutine waits for outgoing packets, link protocol traffic, or sends idle keep-alive traffic
		send := func(msg []byte) {
			msgLen := wire_encode_uint64(uint64(len(msg)))
			buf := net.Buffers{streamMsg[:], msgLen, msg}
			buf.WriteTo(sock)
			atomic.AddUint64(&p.bytesSent, uint64(len(streamMsg)+len(msgLen)+len(msg)))
			util.PutBytes(msg)
		}
		timerInterval := tcp_ping_interval
		timer := time.NewTimer(timerInterval)
		defer timer.Stop()
		for {
			select {
			case msg := <-p.linkOut:
				// Always send outgoing link traffic first, if needed
				send(msg)
				continue
			default:
			}
			// Otherwise wait reset the timer and wait for something to do
			timer.Stop()
			select {
			case <-timer.C:
			default:
			}
			timer.Reset(timerInterval)
			select {
			case _ = <-timer.C:
				send(nil) // TCP keep-alive traffic
			case msg := <-p.linkOut:
				send(msg)
			case msg, ok := <-out:
				if !ok {
					return
				}
				send(msg) // Block until the socket write has finished
				// Now inform the switch that we're ready for more traffic
				p.core.switchTable.idleIn <- p.port
			}
		}
	}()
	p.core.switchTable.idleIn <- p.port // Start in the idle state
	p.out = func(msg []byte) {
		defer func() { recover() }()
		out <- msg
	}
	p.close = func() { sock.Close() }
	go p.linkLoop()
	defer func() {
		// Put all of our cleanup here...
		p.core.peers.removePeer(p.port)
	}()
	us, _, _ := net.SplitHostPort(sock.LocalAddr().String())
	them, _, _ := net.SplitHostPort(sock.RemoteAddr().String())
	themNodeID := crypto.GetNodeID(&meta.box)
	themAddr := address.AddrForNodeID(themNodeID)
	themAddrString := net.IP(themAddr[:]).String()
	themString := fmt.Sprintf("%s@%s", themAddrString, them)
	iface.core.log.Printf("Connected: %s, source: %s", themString, us)
	//iface.stream.init(sock, p.handlePacket)
	bs := make([]byte, 2*streamMsgSize)
	var n int
	for {
		if iface.timeout > 0 {
			sock.SetReadDeadline(time.Now().Add(iface.timeout))
		}
		n, err = sock.Read(bs)
		if err != nil {
			break
		}
		if n > 0 {
			//iface.stream.handleInput(bs[:n])
		}
	}
	if err == nil {
		iface.core.log.Printf("Disconnected: %s, source: %s", themString, us)
	} else {
		iface.core.log.Printf("Disconnected: %s, source: %s, error: %s", themString, us, err)
	}
	return
}
