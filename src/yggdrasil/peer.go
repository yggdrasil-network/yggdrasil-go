package yggdrasil

// TODO cleanup, this file is kind of a mess
//  Commented code should be removed
//  Live code should be better commented

// FIXME (!) this part may be at least sligtly vulnerable to replay attacks
//  The switch message part should catch / drop old tstamps
//  So the damage is limited
//  But you could still mess up msgAnc / msgHops and break some things there
//  It needs to ignore messages with a lower seq
//  Probably best to start setting seq to a timestamp in that case...

import "time"
import "sync"
import "sync/atomic"

//import "fmt"

type peers struct {
	core  *Core
	mutex sync.Mutex   // Synchronize writes to atomic
	ports atomic.Value //map[Port]*peer, use CoW semantics
	//ports map[Port]*peer
	authMutex                   sync.RWMutex
	allowedEncryptionPublicKeys map[boxPubKey]struct{}
}

func (ps *peers) init(c *Core) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	ps.putPorts(make(map[switchPort]*peer))
	ps.core = c
	ps.allowedEncryptionPublicKeys = make(map[boxPubKey]struct{})
}

func (ps *peers) isAllowedEncryptionPublicKey(box *boxPubKey) bool {
	ps.authMutex.RLock()
	defer ps.authMutex.RUnlock()
	_, isIn := ps.allowedEncryptionPublicKeys[*box]
	return isIn || len(ps.allowedEncryptionPublicKeys) == 0
}

func (ps *peers) addAllowedEncryptionPublicKey(box *boxPubKey) {
	ps.authMutex.Lock()
	defer ps.authMutex.Unlock()
	ps.allowedEncryptionPublicKeys[*box] = struct{}{}
}

func (ps *peers) removeAllowedEncryptionPublicKey(box *boxPubKey) {
	ps.authMutex.Lock()
	defer ps.authMutex.Unlock()
	delete(ps.allowedEncryptionPublicKeys, *box)
}

func (ps *peers) getAllowedEncryptionPublicKeys() []boxPubKey {
	ps.authMutex.RLock()
	defer ps.authMutex.RUnlock()
	keys := make([]boxPubKey, 0, len(ps.allowedEncryptionPublicKeys))
	for key := range ps.allowedEncryptionPublicKeys {
		keys = append(keys, key)
	}
	return keys
}

func (ps *peers) getPorts() map[switchPort]*peer {
	return ps.ports.Load().(map[switchPort]*peer)
}

func (ps *peers) putPorts(ports map[switchPort]*peer) {
	ps.ports.Store(ports)
}

type peer struct {
	queueSize  int64  // used to track local backpressure
	bytesSent  uint64 // To track bandwidth usage for getPeers
	bytesRecvd uint64 // To track bandwidth usage for getPeers
	// BUG: sync/atomic, 32 bit platforms need the above to be the first element
	core      *Core
	port      switchPort
	box       boxPubKey
	sig       sigPubKey
	shared    boxSharedKey
	firstSeen time.Time       // To track uptime for getPeers
	linkOut   (chan []byte)   // used for protocol traffic (to bypass queues)
	doSend    (chan struct{}) // tell the linkLoop to send a switchMsg
	dinfo     *dhtInfo        // used to keep the DHT working
	out       func([]byte)    // Set up by whatever created the peers struct, used to send packets to other nodes
	close     func()          // Called when a peer is removed, to close the underlying connection, or via admin api
}

func (p *peer) getQueueSize() int64 {
	return atomic.LoadInt64(&p.queueSize)
}

func (p *peer) updateQueueSize(delta int64) {
	atomic.AddInt64(&p.queueSize, delta)
}

func (ps *peers) newPeer(box *boxPubKey, sig *sigPubKey) *peer {
	now := time.Now()
	p := peer{box: *box,
		sig:       *sig,
		shared:    *getSharedKey(&ps.core.boxPriv, box),
		firstSeen: now,
		doSend:    make(chan struct{}, 1),
		core:      ps.core}
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	oldPorts := ps.getPorts()
	newPorts := make(map[switchPort]*peer)
	for k, v := range oldPorts {
		newPorts[k] = v
	}
	for idx := switchPort(0); true; idx++ {
		if _, isIn := newPorts[idx]; !isIn {
			p.port = switchPort(idx)
			newPorts[p.port] = &p
			break
		}
	}
	ps.putPorts(newPorts)
	return &p
}

func (ps *peers) removePeer(port switchPort) {
	if port == 0 {
		return
	} // Can't remove self peer
	ps.core.router.doAdmin(func() {
		ps.core.switchTable.removePeer(port)
	})
	ps.mutex.Lock()
	oldPorts := ps.getPorts()
	p, isIn := oldPorts[port]
	newPorts := make(map[switchPort]*peer)
	for k, v := range oldPorts {
		newPorts[k] = v
	}
	delete(newPorts, port)
	ps.putPorts(newPorts)
	ps.mutex.Unlock()
	if isIn {
		if p.close != nil {
			p.close()
		}
		close(p.doSend)
	}
}

func (ps *peers) sendSwitchMsgs() {
	ports := ps.getPorts()
	for _, p := range ports {
		if p.port == 0 {
			continue
		}
		select {
		case p.doSend <- struct{}{}:
		default:
		}
	}
}

func (p *peer) linkLoop() {
	go func() { p.doSend <- struct{}{} }()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case _, ok := <-p.doSend:
			if !ok {
				return
			}
			p.sendSwitchMsg()
		case _ = <-tick.C:
			if p.dinfo != nil {
				p.core.dht.peers <- p.dinfo
			}
		}
	}
}

func (p *peer) handlePacket(packet []byte) {
	// TODO See comment in sendPacket about atomics technically being done wrong
	atomic.AddUint64(&p.bytesRecvd, uint64(len(packet)))
	pType, pTypeLen := wire_decode_uint64(packet)
	if pTypeLen == 0 {
		return
	}
	switch pType {
	case wire_Traffic:
		p.handleTraffic(packet, pTypeLen)
	case wire_ProtocolTraffic:
		p.handleTraffic(packet, pTypeLen)
	case wire_LinkProtocolTraffic:
		p.handleLinkTraffic(packet)
	default:
		return
	}
}

func (p *peer) handleTraffic(packet []byte, pTypeLen int) {
	if p.port != 0 && p.dinfo == nil {
		// Drop traffic until the peer manages to send us at least one good switchMsg
		return
	}
	coords, coordLen := wire_decode_coords(packet[pTypeLen:])
	if coordLen >= len(packet) {
		return
	} // No payload
	toPort := p.core.switchTable.lookup(coords)
	if toPort == p.port {
		return
	}
	to := p.core.peers.getPorts()[toPort]
	if to == nil {
		return
	}
	to.sendPacket(packet)
}

func (p *peer) sendPacket(packet []byte) {
	// Is there ever a case where something more complicated is needed?
	// What if p.out blocks?
	p.out(packet)
	// TODO this should really happen at the interface, to account for LIFO packet drops and additional per-packet/per-message overhead, but this should be pretty close... better to move it to the tcp/udp stuff *after* rewriting both to give a common interface
	atomic.AddUint64(&p.bytesSent, uint64(len(packet)))
}

func (p *peer) sendLinkPacket(packet []byte) {
	bs, nonce := boxSeal(&p.shared, packet, nil)
	linkPacket := wire_linkProtoTrafficPacket{
		Nonce:   *nonce,
		Payload: bs,
	}
	packet = linkPacket.encode()
	p.linkOut <- packet
}

func (p *peer) handleLinkTraffic(bs []byte) {
	packet := wire_linkProtoTrafficPacket{}
	if !packet.decode(bs) {
		return
	}
	payload, isOK := boxOpen(&p.shared, packet.Payload, &packet.Nonce)
	if !isOK {
		return
	}
	pType, pTypeLen := wire_decode_uint64(payload)
	if pTypeLen == 0 {
		return
	}
	switch pType {
	case wire_SwitchMsg:
		p.handleSwitchMsg(payload)
	default: // TODO?...
	}
}

func (p *peer) sendSwitchMsg() {
	msg := p.core.switchTable.getMsg()
	if msg == nil {
		return
	}
	bs := getBytesForSig(&p.sig, msg)
	msg.Hops = append(msg.Hops, switchMsgHop{
		Port: p.port,
		Next: p.sig,
		Sig:  *sign(&p.core.sigPriv, bs),
	})
	packet := msg.encode()
	//p.core.log.Println("Encoded msg:", msg, "; bytes:", packet)
	//fmt.Println("Encoded msg:", msg, "; bytes:", packet)
	p.sendLinkPacket(packet)
}

func (p *peer) handleSwitchMsg(packet []byte) {
	var msg switchMsg
	if !msg.decode(packet) {
		return
	}
	//p.core.log.Println("Decoded msg:", msg, "; bytes:", packet)
	if len(msg.Hops) < 1 {
		p.core.peers.removePeer(p.port)
	}
	var loc switchLocator
	prevKey := msg.Root
	for idx, hop := range msg.Hops {
		// Check signatures and collect coords for dht
		sigMsg := msg
		sigMsg.Hops = msg.Hops[:idx]
		loc.coords = append(loc.coords, hop.Port)
		bs := getBytesForSig(&hop.Next, &sigMsg)
		if !p.core.sigs.check(&prevKey, &hop.Sig, bs) {
			p.core.peers.removePeer(p.port)
		}
		prevKey = hop.Next
	}
	p.core.switchTable.handleMsg(&msg, p.port)
	// Pass a mesage to the dht informing it that this peer (still) exists
	loc.coords = loc.coords[:len(loc.coords)-1]
	dinfo := dhtInfo{
		key:    p.box,
		coords: loc.getCoords(),
	}
	p.core.dht.peers <- &dinfo
	p.dinfo = &dinfo
}

func getBytesForSig(next *sigPubKey, msg *switchMsg) []byte {
	var loc switchLocator
	for _, hop := range msg.Hops {
		loc.coords = append(loc.coords, hop.Port)
	}
	bs := append([]byte(nil), next[:]...)
	bs = append(bs, msg.Root[:]...)
	bs = append(bs, wire_encode_uint64(wire_intToUint(msg.TStamp))...)
	bs = append(bs, wire_encode_coords(loc.getCoords())...)
	return bs
}
