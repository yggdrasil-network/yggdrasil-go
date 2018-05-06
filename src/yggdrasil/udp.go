package yggdrasil

// This communicates with peers via UDP
// It's not as well tested or debugged as the TCP transport
// It's intended to use UDP, so debugging/optimzing this is a high priority
// TODO? use golang.org/x/net/ipv6.PacketConn's ReadBatch and WriteBatch?
//  To send all chunks of a message / recv all available chunks in one syscall
//  That might be faster on supported platforms, but it needs investigation
// Chunks are currently murged, but outgoing messages aren't chunked
// This is just to support chunking in the future, if it's needed and debugged
//  Basically, right now we might send UDP packets that are too large

// TODO remove old/unused code and better document live code

import "net"
import "time"
import "sync"
import "fmt"

type udpInterface struct {
	core  *Core
	sock  *net.UDPConn // Or more general PacketConn?
	mutex sync.RWMutex // each conn has an owner goroutine
	conns map[connAddr]*connInfo
}

type connAddr struct {
	ip   [16]byte
	port int
	zone string
}

func (c *connAddr) fromUDPAddr(u *net.UDPAddr) {
	copy(c.ip[:], u.IP.To16())
	c.port = u.Port
	c.zone = u.Zone
}

func (c *connAddr) toUDPAddr() *net.UDPAddr {
	var u net.UDPAddr
	u.IP = make([]byte, 16)
	copy(u.IP, c.ip[:])
	u.Port = c.port
	u.Zone = c.zone
	return &u
}

type connInfo struct {
	name      string
	addr      connAddr
	peer      *peer
	linkIn    chan []byte
	keysIn    chan *udpKeys
	closeIn   chan *udpKeys
	timeout   int // count of how many heartbeats have been missed
	in        func([]byte)
	out       chan []byte
	countIn   uint8
	countOut  uint8
	chunkSize uint16
}

type udpKeys struct {
	box boxPubKey
	sig sigPubKey
}

func (iface *udpInterface) init(core *Core, addr string) {
	iface.core = core
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		panic(err)
	}
	iface.sock, err = net.ListenUDP("udp", udpAddr)
	if err != nil {
		panic(err)
	}
	iface.conns = make(map[connAddr]*connInfo)
	go iface.reader()
}

func (iface *udpInterface) sendKeys(addr connAddr) {
	udpAddr := addr.toUDPAddr()
	msg := []byte{}
	msg = udp_encode(msg, 0, 0, 0, nil)
	msg = append(msg, iface.core.boxPub[:]...)
	msg = append(msg, iface.core.sigPub[:]...)
	iface.sock.WriteToUDP(msg, udpAddr)
}

func (iface *udpInterface) sendClose(addr connAddr) {
	udpAddr := addr.toUDPAddr()
	msg := []byte{}
	msg = udp_encode(msg, 0, 1, 0, nil)
	msg = append(msg, iface.core.boxPub[:]...)
	msg = append(msg, iface.core.sigPub[:]...)
	iface.sock.WriteToUDP(msg, udpAddr)
}

func udp_isKeys(msg []byte) bool {
	keyLen := 3 + boxPubKeyLen + sigPubKeyLen
	return len(msg) == keyLen && msg[0] == 0x00 && msg[1] == 0x00
}

func udp_isClose(msg []byte) bool {
	keyLen := 3 + boxPubKeyLen + sigPubKeyLen
	return len(msg) == keyLen && msg[0] == 0x00 && msg[1] == 0x01
}

func (iface *udpInterface) startConn(info *connInfo) {
	ticker := time.NewTicker(6 * time.Second)
	defer ticker.Stop()
	defer func() {
		// Cleanup
		iface.mutex.Lock()
		delete(iface.conns, info.addr)
		iface.mutex.Unlock()
		iface.core.peers.removePeer(info.peer.port)
		close(info.linkIn)
		close(info.keysIn)
		close(info.closeIn)
		close(info.out)
		iface.core.log.Println("Removing peer:", info.name)
	}()
	for {
		select {
		case ks := <-info.closeIn:
			{
				if ks.box == info.peer.box && ks.sig == info.peer.sig {
					// TODO? secure this somehow
					//  Maybe add a signature and sequence number (timestamp) to close and keys?
					return
				}
			}
		case ks := <-info.keysIn:
			{
				// FIXME? need signatures/sequence-numbers or something
				// Spoofers could lock out a peer with fake/bad keys
				if ks.box == info.peer.box && ks.sig == info.peer.sig {
					info.timeout = 0
				}
			}
		case <-ticker.C:
			{
				if info.timeout > 10 {
					return
				}
				info.timeout++
				iface.sendKeys(info.addr)
			}
		}
	}
}

func (iface *udpInterface) handleClose(msg []byte, addr connAddr) {
	//defer util_putBytes(msg)
	var ks udpKeys
	_, _, _, bs := udp_decode(msg)
	switch {
	case !wire_chop_slice(ks.box[:], &bs):
		return
	case !wire_chop_slice(ks.sig[:], &bs):
		return
	}
	if ks.box == iface.core.boxPub {
		return
	}
	if ks.sig == iface.core.sigPub {
		return
	}
	iface.mutex.RLock()
	conn, isIn := iface.conns[addr]
	iface.mutex.RUnlock()
	if !isIn {
		return
	}
	func() {
		defer func() { recover() }()
		select {
		case conn.closeIn <- &ks:
		default:
		}
	}()
}

func (iface *udpInterface) handleKeys(msg []byte, addr connAddr) {
	//defer util_putBytes(msg)
	var ks udpKeys
	_, _, _, bs := udp_decode(msg)
	switch {
	case !wire_chop_slice(ks.box[:], &bs):
		return
	case !wire_chop_slice(ks.sig[:], &bs):
		return
	}
	if ks.box == iface.core.boxPub {
		return
	}
	if ks.sig == iface.core.sigPub {
		return
	}
	iface.mutex.RLock()
	conn, isIn := iface.conns[addr]
	iface.mutex.RUnlock()
	if !isIn {
		udpAddr := addr.toUDPAddr()
		themNodeID := getNodeID(&ks.box)
		themAddr := address_addrForNodeID(themNodeID)
		themAddrString := net.IP(themAddr[:]).String()
		themString := fmt.Sprintf("%s@%s", themAddrString, udpAddr.String())
		conn = &connInfo{
			name:      themString,
			addr:      connAddr(addr),
			peer:      iface.core.peers.newPeer(&ks.box, &ks.sig),
			linkIn:    make(chan []byte, 1),
			keysIn:    make(chan *udpKeys, 1),
			closeIn:   make(chan *udpKeys, 1),
			out:       make(chan []byte, 32),
			chunkSize: 576 - 60 - 8 - 3, // max safe - max ip - udp header - chunk overhead
		}
		if udpAddr.IP.IsLinkLocalUnicast() {
			ifce, err := net.InterfaceByName(udpAddr.Zone)
			if ifce != nil && err == nil {
				conn.chunkSize = uint16(ifce.MTU) - 60 - 8 - 3
			}
		}
		var inChunks uint8
		var inBuf []byte
		conn.in = func(bs []byte) {
			//defer util_putBytes(bs)
			chunks, chunk, count, payload := udp_decode(bs)
			if count != conn.countIn {
				if len(inBuf) > 0 {
					// Something went wrong
					// Forward whatever we have
					// Maybe the destination can do something about it
					msg := append(util_getBytes(), inBuf...)
					conn.peer.handlePacket(msg, conn.linkIn)
				}
				inChunks = 0
				inBuf = inBuf[:0]
				conn.countIn = count
			}
			if chunk <= chunks && chunk == inChunks+1 {
				inChunks += 1
				inBuf = append(inBuf, payload...)
				if chunks != chunk {
					return
				}
				msg := append(util_getBytes(), inBuf...)
				conn.peer.handlePacket(msg, conn.linkIn)
				inBuf = inBuf[:0]
			}
		}
		conn.peer.out = func(msg []byte) {
			defer func() { recover() }()
			select {
			case conn.out <- msg:
			default:
				util_putBytes(msg)
			}
		}
		go func() {
			var out []byte
			var chunks [][]byte
			for msg := range conn.out {
				chunks = chunks[:0]
				bs := msg
				for len(bs) > int(conn.chunkSize) {
					chunks, bs = append(chunks, bs[:conn.chunkSize]), bs[conn.chunkSize:]
				}
				chunks = append(chunks, bs)
				if len(chunks) > 255 {
					continue
				}
				start := time.Now()
				for idx, bs := range chunks {
					nChunks, nChunk, count := uint8(len(chunks)), uint8(idx)+1, conn.countOut
					out = udp_encode(out[:0], nChunks, nChunk, count, bs)
					//iface.core.log.Println("DEBUG out:", nChunks, nChunk, count, len(bs))
					iface.sock.WriteToUDP(out, udpAddr)
				}
				timed := time.Since(start)
				conn.countOut += 1
				conn.peer.updateBandwidth(len(msg), timed)
				util_putBytes(msg)
			}
		}()
		//*/
		conn.peer.close = func() { iface.sendClose(conn.addr) }
		iface.mutex.Lock()
		iface.conns[addr] = conn
		iface.mutex.Unlock()
		iface.core.log.Println("Adding peer:", conn.name)
		go iface.startConn(conn)
		go conn.peer.linkLoop(conn.linkIn)
		iface.sendKeys(conn.addr)
	}
	func() {
		defer func() { recover() }()
		select {
		case conn.keysIn <- &ks:
		default:
		}
	}()
}

func (iface *udpInterface) handlePacket(msg []byte, addr connAddr) {
	iface.mutex.RLock()
	if conn, isIn := iface.conns[addr]; isIn {
		conn.in(msg)
	}
	iface.mutex.RUnlock()
}

func (iface *udpInterface) reader() {
	iface.core.log.Println("Listening for UDP on:", iface.sock.LocalAddr().String())
	bs := make([]byte, 65536) // This needs to be large enough for everything...
	for {
		n, udpAddr, err := iface.sock.ReadFromUDP(bs)
		//iface.core.log.Println("DEBUG: read:", bs[0], bs[1], bs[2], n)
		if err != nil {
			panic(err)
			break
		}
		msg := bs[:n]
		var addr connAddr
		addr.fromUDPAddr(udpAddr)
		switch {
		case udp_isKeys(msg):
			var them address
			copy(them[:], udpAddr.IP.To16())
			if them.isValid() {
				continue
			}
			if udpAddr.IP.IsLinkLocalUnicast() &&
				!iface.core.ifceExpr.MatchString(udpAddr.Zone) {
				continue
			}
			iface.handleKeys(msg, addr)
		case udp_isClose(msg):
			iface.handleClose(msg, addr)
		default:
			iface.handlePacket(msg, addr)
		}
	}
}

////////////////////////////////////////////////////////////////////////////////

func udp_decode(bs []byte) (chunks, chunk, count uint8, payload []byte) {
	if len(bs) >= 3 {
		chunks, chunk, count, payload = bs[0], bs[1], bs[2], bs[3:]
	}
	return
}

func udp_encode(out []byte, chunks, chunk, count uint8, payload []byte) []byte {
	return append(append(out, chunks, chunk, count), payload...)
}
