package yggdrasil

// This sends packets to peers using TCP as a transport
// It's generally better tested than the UDP implementation
// Using it regularly is insane, but I find TCP easier to test/debug with it
// Updating and optimizing the UDP version is a higher priority

// TODO:
//  Something needs to make sure we're getting *valid* packets
//  Could be used to DoS (connect, give someone else's keys, spew garbage)
//  I guess the "peer" part should watch for link packets, disconnect?

import "net"
import "time"
import "errors"
import "sync"
import "fmt"
import "golang.org/x/net/proxy"

const tcp_msgSize = 2048 + 65535 // TODO figure out what makes sense

// wrapper function for non tcp/ip connections
func setNoDelay(c net.Conn, delay bool) {
	tcp, ok := c.(*net.TCPConn)
	if ok {
		tcp.SetNoDelay(delay)
	}
}

type tcpInterface struct {
	core  *Core
	serv  net.Listener
	mutex sync.Mutex // Protecting the below
	calls map[string]struct{}
	conns map[tcpInfo](chan struct{})
}

type tcpInfo struct {
	box        boxPubKey
	sig        sigPubKey
	localAddr  string
	remoteAddr string
}

func (iface *tcpInterface) getAddr() *net.TCPAddr {
	return iface.serv.Addr().(*net.TCPAddr)
}

func (iface *tcpInterface) connect(addr string) {
	iface.call(addr)
}

func (iface *tcpInterface) connectSOCKS(socksaddr, peeraddr string) {
	go func() {
		dialer, err := proxy.SOCKS5("tcp", socksaddr, nil, proxy.Direct)
		if err == nil {
			conn, err := dialer.Dial("tcp", peeraddr)
			if err == nil {
				iface.callWithConn(&wrappedConn{
					c: conn,
					raddr: &wrappedAddr{
						network: "tcp",
						addr:    peeraddr,
					},
				})
			}
		}
	}()
}

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

func (iface *tcpInterface) callWithConn(conn net.Conn) {
	go func() {
		raddr := conn.RemoteAddr().String()
		iface.mutex.Lock()
		_, isIn := iface.calls[raddr]
		iface.mutex.Unlock()
		if !isIn {
			iface.mutex.Lock()
			iface.calls[raddr] = struct{}{}
			iface.mutex.Unlock()
			defer func() {
				iface.mutex.Lock()
				delete(iface.calls, raddr)
				iface.mutex.Unlock()
			}()
			iface.handler(conn, false)
		}
	}()
}

func (iface *tcpInterface) call(saddr string) {
	go func() {
		quit := false
		iface.mutex.Lock()
		if _, isIn := iface.calls[saddr]; isIn {
			quit = true
		} else {
			iface.calls[saddr] = struct{}{}
			defer func() {
				iface.mutex.Lock()
				delete(iface.calls, saddr)
				iface.mutex.Unlock()
			}()
		}
		iface.mutex.Unlock()
		if !quit {
			conn, err := net.Dial("tcp", saddr)
			if err != nil {
				return
			}
			iface.handler(conn, false)
		}
	}()
}

func (iface *tcpInterface) handler(sock net.Conn, incoming bool) {
	defer sock.Close()
	// Get our keys
	keys := []byte{}
	keys = append(keys, tcp_key[:]...)
	keys = append(keys, iface.core.boxPub[:]...)
	keys = append(keys, iface.core.sigPub[:]...)
	_, err := sock.Write(keys)
	if err != nil {
		return
	}
	timeout := time.Now().Add(6 * time.Second)
	sock.SetReadDeadline(timeout)
	n, err := sock.Read(keys)
	if err != nil {
		return
	}
	if n < len(keys) { /*panic("Partial key packet?") ;*/
		return
	}
	info := tcpInfo{}
	if !tcp_chop_keys(&info.box, &info.sig, &keys) { /*panic("Invalid key packet?") ;*/
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
	if equiv(info.box[:], iface.core.boxPub[:]) {
		return
	} // testing
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
	p := iface.core.peers.newPeer(&info.box, &info.sig)
	p.linkOut = make(chan []byte, 1)
	in := func(bs []byte) {
		p.handlePacket(bs)
	}
	out := make(chan []byte, 32) // TODO? what size makes sense
	defer close(out)
	go func() {
		var shadow uint64
		var stack [][]byte
		put := func(msg []byte) {
			stack = append(stack, msg)
			for len(stack) > 32 {
				util_putBytes(stack[0])
				stack = stack[1:]
				shadow++
			}
		}
		send := func(msg []byte) {
			msgLen := wire_encode_uint64(uint64(len(msg)))
			buf := net.Buffers{tcp_msg[:], msgLen, msg}
			buf.WriteTo(sock)
			util_putBytes(msg)
		}
		timerInterval := 4 * time.Second
		timer := time.NewTimer(timerInterval)
		defer timer.Stop()
		for {
			for ; shadow > 0; shadow-- {
				p.updateQueueSize(-1)
			}
			timer.Stop()
			select {
			case <-timer.C:
			default:
			}
			timer.Reset(timerInterval)
			select {
			case _ = <-timer.C:
				//iface.core.log.Println("DEBUG: sending keep-alive:", sock.RemoteAddr().String())
				send(nil) // TCP keep-alive traffic
			case msg := <-p.linkOut:
				send(msg)
			case msg, ok := <-out:
				if !ok {
					return
				}
				put(msg)
			}
			for len(stack) > 0 {
				select {
				case msg := <-p.linkOut:
					send(msg)
				case msg, ok := <-out:
					if !ok {
						return
					}
					put(msg)
				default:
					msg := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					send(msg)
					p.updateQueueSize(-1)
				}
			}
		}
	}()
	p.out = func(msg []byte) {
		defer func() { recover() }()
		select {
		case out <- msg:
			p.updateQueueSize(1)
		default:
			util_putBytes(msg)
		}
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

func (iface *tcpInterface) reader(sock net.Conn, in func([]byte)) {
	bs := make([]byte, 2*tcp_msgSize)
	frag := bs[:0]
	for {
		timeout := time.Now().Add(6 * time.Second)
		sock.SetReadDeadline(timeout)
		n, err := sock.Read(bs[len(frag):])
		if err != nil || n == 0 {
			//	iface.core.log.Println(err)
			break
		}
		frag = bs[:len(frag)+n]
		for {
			msg, ok, err := tcp_chop_msg(&frag)
			if err != nil {
				//	iface.core.log.Println(err)
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

// Magic bytes to check
var tcp_key = [...]byte{'k', 'e', 'y', 's'}
var tcp_msg = [...]byte{0xde, 0xad, 0xb1, 0x75} // "dead bits"

func tcp_chop_keys(box *boxPubKey, sig *sigPubKey, bs *[]byte) bool {
	// This one is pretty simple: we know how long the message should be
	// So don't call this with a message that's too short
	if len(*bs) < len(tcp_key)+len(*box)+len(*sig) {
		return false
	}
	for idx := range tcp_key {
		if (*bs)[idx] != tcp_key[idx] {
			return false
		}
	}
	(*bs) = (*bs)[len(tcp_key):]
	copy(box[:], *bs)
	(*bs) = (*bs)[len(box):]
	copy(sig[:], *bs)
	(*bs) = (*bs)[len(sig):]
	return true
}

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
