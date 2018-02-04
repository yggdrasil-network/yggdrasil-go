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

const tcp_msgSize = 2048 + 65535 // TODO figure out what makes sense

type tcpInterface struct {
	core  *Core
	serv  *net.TCPListener
	mutex sync.Mutex // Protecting the below
	calls map[string]struct{}
}

type tcpKeys struct {
	box boxPubKey
	sig sigPubKey
}

func (iface *tcpInterface) init(core *Core, addr string) {
	iface.core = core
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		panic(err)
	}
	iface.serv, err = net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		panic(err)
	}
	iface.calls = make(map[string]struct{})
	go iface.listener()
}

func (iface *tcpInterface) listener() {
	defer iface.serv.Close()
	iface.core.log.Println("Listening on:", iface.serv.Addr().String())
	for {
		sock, err := iface.serv.AcceptTCP()
		if err != nil {
			panic(err)
		}
		go iface.handler(sock)
	}
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
			conn, err := net.DialTimeout("tcp", saddr, 6*time.Second)
			if err != nil {
				return
			}
			sock := conn.(*net.TCPConn)
			iface.handler(sock)
		}
	}()
}

func (iface *tcpInterface) handler(sock *net.TCPConn) {
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
	ks := tcpKeys{}
	if !tcp_chop_keys(&ks.box, &ks.sig, &keys) { /*panic("Invalid key packet?") ;*/
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
	if equiv(ks.box[:], iface.core.boxPub[:]) {
		return
	} // testing
	if equiv(ks.sig[:], iface.core.sigPub[:]) {
		return
	}
	// Note that multiple connections to the same node are allowed
	//  E.g. over different interfaces
	linkIn := make(chan []byte, 1)
	p := iface.core.peers.newPeer(&ks.box, &ks.sig) //, in, out)
	in := func(bs []byte) {
		p.handlePacket(bs, linkIn)
	}
	out := make(chan []byte, 32) // TODO? what size makes sense
	defer close(out)
	go func() {
		var stack [][]byte
		put := func(msg []byte) {
			stack = append(stack, msg)
			for len(stack) > 32 {
				util_putBytes(stack[0])
				stack = stack[1:]
			}
		}
		send := func() {
			msg := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			buf := net.Buffers{tcp_msg[:],
				wire_encode_uint64(uint64(len(msg))),
				msg}
			size := 0
			for _, bs := range buf {
				size += len(bs)
			}
			start := time.Now()
			buf.WriteTo(sock)
			timed := time.Since(start)
			pType, _ := wire_decode_uint64(msg)
			if pType == wire_LinkProtocolTraffic {
				p.updateBandwidth(size, timed)
			}
			util_putBytes(msg)
		}
		for msg := range out {
			put(msg)
			for len(stack) > 0 {
				// Keep trying to fill the stack (LIFO order) while sending
				select {
				case msg, ok := <-out:
					if !ok {
						return
					}
					put(msg)
				default:
					send()
				}
			}
		}
	}()
	p.out = func(msg []byte) {
		defer func() { recover() }()
		for {
			select {
			case out <- msg:
				return
			default:
				util_putBytes(<-out)
			}
		}
	}
	sock.SetNoDelay(true)
	go p.linkLoop(linkIn)
	defer func() {
		// Put all of our cleanup here...
		p.core.peers.mutex.Lock()
		oldPorts := p.core.peers.getPorts()
		newPorts := make(map[switchPort]*peer)
		for k, v := range oldPorts {
			newPorts[k] = v
		}
		delete(newPorts, p.port)
		p.core.peers.putPorts(newPorts)
		p.core.peers.mutex.Unlock()
		close(linkIn)
	}()
	them := sock.RemoteAddr()
	themNodeID := getNodeID(&ks.box)
	themAddr := address_addrForNodeID(themNodeID)
	themAddrString := net.IP(themAddr[:]).String()
	themString := fmt.Sprintf("%s@%s", themAddrString, them)
	iface.core.log.Println("Connected:", themString)
	iface.reader(sock, in) // In this goroutine, because of defers
	iface.core.log.Println("Disconnected:", themString)
	return
}

func (iface *tcpInterface) reader(sock *net.TCPConn, in func([]byte)) {
	bs := make([]byte, 2*tcp_msgSize)
	frag := bs[:0]
	for {
		timeout := time.Now().Add(6 * time.Second)
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
