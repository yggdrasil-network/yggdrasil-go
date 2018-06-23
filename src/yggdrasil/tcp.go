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
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"
)

const tcp_msgSize = 2048 + 65535 // TODO figure out what makes sense
const tcp_timeout = 6 * time.Second

// Wrapper function for non tcp/ip connections.
func setNoDelay(c net.Conn, delay bool) {
	tcp, ok := c.(*net.TCPConn)
	if ok {
		tcp.SetNoDelay(delay)
	}
}

// The TCP listener and information about active TCP connections, to avoid duplication.
type tcpInterface struct {
	core  *Core
	serv  net.Listener
	mutex sync.Mutex // Protecting the below
	calls map[string]struct{}
	conns map[tcpInfo](chan struct{})
}

// This is used as the key to a map that tracks existing connections, to prevent multiple connections to the same keys and local/remote address pair from occuring.
// Different address combinations are allowed, so multi-homing is still technically possible (but not necessarily advisable).
type tcpInfo struct {
	box        boxPubKey
	sig        sigPubKey
	localAddr  string
	remoteAddr string
}

// Returns the address of the listener.
func (iface *tcpInterface) getAddr() *net.TCPAddr {
	return iface.serv.Addr().(*net.TCPAddr)
}

// Attempts to initiate a connection to the provided address.
func (iface *tcpInterface) connect(addr string) {
	iface.call(addr, nil)
}

// Attempst to initiate a connection to the provided address, viathe provided socks proxy address.
func (iface *tcpInterface) connectSOCKS(socksaddr, peeraddr string) {
	iface.call(peeraddr, &socksaddr)
}

// Initializes the struct.
func (iface *tcpInterface) init(core *Core, addr string) (err error) {
	iface.core = core

	iface.serv, err = net.Listen("tcp", addr)
	if err == nil {
		iface.calls = make(map[string]struct{})
		iface.conns = make(map[tcpInfo](chan struct{}))
		go iface.listener()
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
			panic(err)
		}
		go iface.handler(sock, true)
	}
}

// Checks if a connection already exists.
// If not, it adds it to the list of active outgoing calls (to block future attempts) and dials the address.
// If the dial is successful, it launches the handler.
// When finished, it removes the outgoing call, so reconnection attempts can be made later.
// This all happens in a separate goroutine that it spawns.
func (iface *tcpInterface) call(saddr string, socksaddr *string) {
	go func() {
		quit := false
		iface.mutex.Lock()
		if _, isIn := iface.calls[saddr]; isIn {
			quit = true
		} else {
			iface.calls[saddr] = struct{}{}
			defer func() {
				// Block new calls for a little while, to mitigate livelock scenarios
				time.Sleep(tcp_timeout)
				time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
				iface.mutex.Lock()
				delete(iface.calls, saddr)
				iface.mutex.Unlock()
			}()
		}
		iface.mutex.Unlock()
		if quit {
			return
		}
		var conn net.Conn
		var err error
		if socksaddr != nil {
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
			conn, err = net.Dial("tcp", saddr)
			if err != nil {
				return
			}
		}
		iface.handler(conn, false)
	}()
}

// This exchanges/checks connection metadata, sets up the peer struct, sets up the writer goroutine, and then runs the reader within the current goroutine.
// It defers a bunch of cleanup stuff to tear down all of these things when the reader exists (e.g. due to a closed connection or a timeout).
func (iface *tcpInterface) handler(sock net.Conn, incoming bool) {
	defer sock.Close()
	// Get our keys
	myLinkPub, myLinkPriv := newBoxKeys() // ephemeral link keys
	meta := version_getBaseMetadata()
	meta.box = iface.core.boxPub
	meta.sig = iface.core.sigPub
	meta.link = *myLinkPub
	metaBytes := meta.encode()
	_, err := sock.Write(metaBytes)
	if err != nil {
		return
	}
	timeout := time.Now().Add(tcp_timeout)
	sock.SetReadDeadline(timeout)
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
	info := tcpInfo{ // used as a map key, so don't include ephemeral link key
		box: meta.box,
		sig: meta.sig,
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
	if equiv(info.box[:], iface.core.boxPub[:]) {
		return
	}
	if equiv(info.sig[:], iface.core.sigPub[:]) {
		return
	}
	// Check if we're authorized to connect to this key / IP
	if incoming && !iface.core.peers.isAllowedEncryptionPublicKey(&info.box) {
		// Allow unauthorized peers if they're link-local
		raddrStr, _, _ := net.SplitHostPort(sock.RemoteAddr().String())
		raddr := net.ParseIP(raddrStr)
		if !raddr.IsLinkLocalUnicast() {
			return
		}
	}
	// Check if we already have a connection to this node, close and block if yes
	info.localAddr, _, _ = net.SplitHostPort(sock.LocalAddr().String())
	info.remoteAddr, _, _ = net.SplitHostPort(sock.RemoteAddr().String())
	iface.mutex.Lock()
	if blockChan, isIn := iface.conns[info]; isIn {
		iface.mutex.Unlock()
		sock.Close()
		<-blockChan
		return
	}
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
	p := iface.core.peers.newPeer(&info.box, &info.sig, getSharedKey(myLinkPriv, &meta.link))
	p.linkOut = make(chan []byte, 1)
	in := func(bs []byte) {
		p.handlePacket(bs)
	}
	out := make(chan []byte, 1024) // Should be effectively infinite, but gets fed into finite LIFO stack
	defer close(out)
	go func() {
		var shadow int64
		var stack [][]byte
		put := func(msg []byte) {
			stack = append(stack, msg)
			sort.SliceStable(stack, func(i, j int) bool {
				// Sort in reverse order, with smallest messages at the end
				return len(stack[i]) >= len(stack[j])
			})
			for len(stack) > 32 {
				util_putBytes(stack[0])
				stack = stack[1:]
				shadow++
			}
		}
		send := make(chan []byte)
		defer close(send)
		go func() {
			for msg := range send {
				msgLen := wire_encode_uint64(uint64(len(msg)))
				buf := net.Buffers{tcp_msg[:], msgLen, msg}
				buf.WriteTo(sock)
				atomic.AddUint64(&p.bytesSent, uint64(len(tcp_msg)+len(msgLen)+len(msg)))
				util_putBytes(msg)
			}
		}()
		timerInterval := tcp_timeout * 2 / 3
		timer := time.NewTimer(timerInterval)
		defer timer.Stop()
		for {
			if shadow != 0 {
				p.updateQueueSize(-shadow)
				shadow = 0
			}
			timer.Stop()
			select {
			case <-timer.C:
			default:
			}
			timer.Reset(timerInterval)
			select {
			case _ = <-timer.C:
				send <- nil // TCP keep-alive traffic
			case msg := <-p.linkOut:
				send <- msg
			case msg, ok := <-out:
				if !ok {
					return
				}
				put(msg)
			}
			for len(stack) > 0 {
				// First make sure linkOut gets sent first, if it's non-empty
				select {
				case msg := <-p.linkOut:
					send <- msg
					continue
				default:
				}
				// Then block until we send or receive something
				select {
				case msg := <-p.linkOut:
					send <- msg
				case msg, ok := <-out:
					if !ok {
						return
					}
					put(msg)
				case send <- stack[len(stack)-1]:
					stack = stack[:len(stack)-1]
					p.updateQueueSize(-1)
				}
			}
		}
	}()
	p.out = func(msg []byte) {
		p.updateQueueSize(1)
		defer func() { recover() }()
		out <- msg
	}
	p.close = func() { sock.Close() }
	setNoDelay(sock, true)
	go p.linkLoop()
	defer func() {
		// Put all of our cleanup here...
		p.core.peers.removePeer(p.port)
	}()
	them, _, _ := net.SplitHostPort(sock.RemoteAddr().String())
	themNodeID := getNodeID(&info.box)
	themAddr := address_addrForNodeID(themNodeID)
	themAddrString := net.IP(themAddr[:]).String()
	themString := fmt.Sprintf("%s@%s", themAddrString, them)
	iface.core.log.Println("Connected:", themString)
	iface.reader(sock, in) // In this goroutine, because of defers
	iface.core.log.Println("Disconnected:", themString)
	return
}

// This reads from the socket into a []byte buffer for incomping messages.
// It copies completed messages out of the cache into a new slice, and passes them to the peer struct via the provided `in func([]byte)` argument.
// Then it shifts the incomplete fragments of data forward so future reads won't overwrite it.
func (iface *tcpInterface) reader(sock net.Conn, in func([]byte)) {
	bs := make([]byte, 2*tcp_msgSize)
	frag := bs[:0]
	for {
		timeout := time.Now().Add(tcp_timeout)
		sock.SetReadDeadline(timeout)
		n, err := sock.Read(bs[len(frag):])
		if err != nil || n == 0 {
			break
		}
		frag = bs[:len(frag)+n]
		for {
			msg, ok, err := tcp_chop_msg(&frag)
			if err != nil {
				return
			}
			if !ok {
				break
			} // We didn't get the whole message yet
			newMsg := append(util_getBytes(), msg...)
			in(newMsg)
			util_yield()
		}
		frag = append(bs[:0], frag...)
	}
}

////////////////////////////////////////////////////////////////////////////////

// These are 4 bytes of padding used to catch if something went horribly wrong with the tcp connection.
var tcp_msg = [...]byte{0xde, 0xad, 0xb1, 0x75} // "dead bits"

// This takes a pointer to a slice as an argument.
// It checks if there's a complete message and, if so, slices out those parts and returns the message, true, and nil.
// If there's no error, but also no complete message, it returns nil, false, and nil.
// If there's an error, it returns nil, false, and the error, which the reader then handles (currently, by returning from the reader, which causes the connection to close).
func tcp_chop_msg(bs *[]byte) ([]byte, bool, error) {
	// Returns msg, ok, err
	if len(*bs) < len(tcp_msg) {
		return nil, false, nil
	}
	for idx := range tcp_msg {
		if (*bs)[idx] != tcp_msg[idx] {
			return nil, false, errors.New("Bad message!")
		}
	}
	msgLen, msgLenLen := wire_decode_uint64((*bs)[len(tcp_msg):])
	if msgLen > tcp_msgSize {
		return nil, false, errors.New("Oversized message!")
	}
	msgBegin := len(tcp_msg) + msgLenLen
	msgEnd := msgBegin + int(msgLen)
	if msgLenLen == 0 || len(*bs) < msgEnd {
		// We don't have the full message
		// Need to buffer this and wait for the rest to come in
		return nil, false, nil
	}
	msg := (*bs)[msgBegin:msgEnd]
	(*bs) = (*bs)[msgEnd:]
	return msg, true, nil
}
