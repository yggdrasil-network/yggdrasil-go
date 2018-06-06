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

// FIXME (!?) if it takes too long to communicate all the msgHops, then things hit a horizon
//  That could happen with a peer over a high-latency link, with many msgHops
//  Possible workarounds:
//    1. Pre-emptively send all hops when one is requested, or after any change
//      Maybe requires changing how the throttle works and msgHops are saved
//      In case some arrive out of order or are dropped
//      This is relatively easy to implement, but could be wasteful
//    2. Save your old locator, sigs, etc, so you can respond to older ancs
//      And finish requesting an old anc before updating to a new one
//      But that may lead to other issues if not done carefully...

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
	// Rolling approximation of bandwidth, in bps, used by switch, updated by packet sends
	// use get/update methods only! (atomic accessors as float64)
	queueSize  int64
	bytesSent  uint64 // To track bandwidth usage for getPeers
	bytesRecvd uint64 // To track bandwidth usage for getPeers
	// BUG: sync/atomic, 32 bit platforms need the above to be the first element
	firstSeen time.Time // To track uptime for getPeers
	box       boxPubKey
	sig       sigPubKey
	shared    boxSharedKey
	//in <-chan []byte
	//out chan<- []byte
	//in func([]byte)
	out     func([]byte)
	core    *Core
	port    switchPort
	msgAnc  *msgAnnounce
	msgHops []*msgHop
	myMsg   *switchMessage
	mySigs  []sigInfo
	// This is used to limit how often we perform expensive operations
	//  Specifically, processing switch messages, signing, and verifying sigs
	//  Resets at the start of each tick
	throttle uint8
	// Called when a peer is removed, to close the underlying connection, or via admin api
	close func()
	// To allow the peer to call close if idle for too long
	lastAnc time.Time
}

const peer_Throttle = 1

func (p *peer) getQueueSize() int64 {
	return atomic.LoadInt64(&p.queueSize)
}

func (p *peer) updateQueueSize(delta int64) {
	atomic.AddInt64(&p.queueSize, delta)
}

func (ps *peers) newPeer(box *boxPubKey,
	sig *sigPubKey) *peer {
	now := time.Now()
	p := peer{box: *box,
		sig:       *sig,
		shared:    *getSharedKey(&ps.core.boxPriv, box),
		lastAnc:   now,
		firstSeen: now,
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
	// TODO? store linkIn in the peer struct, close it here? (once)
	if port == 0 {
		return
	} // Can't remove self peer
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
	if isIn && p.close != nil {
		p.close()
	}
}

func (p *peer) linkLoop(in <-chan []byte) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var counter uint8
	var lastRSeq uint64
	for {
		select {
		case packet, ok := <-in:
			if !ok {
				return
			}
			p.handleLinkTraffic(packet)
		case <-ticker.C:
			if time.Since(p.lastAnc) > 16*time.Second && p.close != nil {
				// Seems to have timed out, try to trigger a close
				p.close()
			}
			p.throttle = 0
			if p.port == 0 {
				continue
			} // Don't send announces on selfInterface
			p.myMsg, p.mySigs = p.core.switchTable.createMessage(p.port)
			var update bool
			switch {
			case p.msgAnc == nil:
				update = true
			case lastRSeq != p.msgAnc.Seq:
				update = true
			case p.msgAnc.Rseq != p.myMsg.seq:
				update = true
			case counter%4 == 0:
				update = true
			}
			if update {
				if p.msgAnc != nil {
					lastRSeq = p.msgAnc.Seq
				}
				p.sendSwitchAnnounce()
			}
			counter = (counter + 1) % 4
		}
	}
}

func (p *peer) handlePacket(packet []byte, linkIn chan<- []byte) {
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
		{
			select {
			case linkIn <- packet:
			default:
			}
		}
	default: /*panic(pType) ;*/
		return
	}
}

func (p *peer) handleTraffic(packet []byte, pTypeLen int) {
	if p.port != 0 && p.msgAnc == nil {
		// Drop traffic until the peer manages to send us at least one anc
		return
	}
	ttl, ttlLen := wire_decode_uint64(packet[pTypeLen:])
	ttlBegin := pTypeLen
	ttlEnd := pTypeLen + ttlLen
	coords, coordLen := wire_decode_coords(packet[ttlEnd:])
	coordEnd := ttlEnd + coordLen
	if coordEnd == len(packet) {
		return
	} // No payload
	toPort, newTTL := p.core.switchTable.lookup(coords, ttl)
	if toPort == p.port {
		return
	}
	to := p.core.peers.getPorts()[toPort]
	if to == nil {
		return
	}
	// This mutates the packet in-place if the length of the TTL changes!
	ttlSlice := wire_encode_uint64(newTTL)
	newTTLLen := len(ttlSlice)
	shift := ttlLen - newTTLLen
	copy(packet[shift:], packet[:pTypeLen])
	copy(packet[ttlBegin+shift:], ttlSlice)
	packet = packet[shift:]
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
	p.sendPacket(packet)
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
	case wire_SwitchAnnounce:
		p.handleSwitchAnnounce(payload)
	case wire_SwitchHopRequest:
		p.handleSwitchHopRequest(payload)
	case wire_SwitchHop:
		p.handleSwitchHop(payload)
	}
}

func (p *peer) handleSwitchAnnounce(packet []byte) {
	//p.core.log.Println("DEBUG: handleSwitchAnnounce")
	anc := msgAnnounce{}
	//err := wire_decode_struct(packet, &anc)
	//if err != nil { return }
	if !anc.decode(packet) {
		return
	}
	//if p.msgAnc != nil && anc.Seq != p.msgAnc.Seq { p.msgHops = nil }
	if p.msgAnc == nil ||
		anc.Root != p.msgAnc.Root ||
		anc.Tstamp != p.msgAnc.Tstamp ||
		anc.Seq != p.msgAnc.Seq {
		p.msgHops = nil
	}
	p.msgAnc = &anc
	p.processSwitchMessage()
	p.lastAnc = time.Now()
}

func (p *peer) requestHop(hop uint64) {
	//p.core.log.Println("DEBUG requestHop")
	req := msgHopReq{}
	req.Root = p.msgAnc.Root
	req.Tstamp = p.msgAnc.Tstamp
	req.Seq = p.msgAnc.Seq
	req.Hop = hop
	packet := req.encode()
	p.sendLinkPacket(packet)
}

func (p *peer) handleSwitchHopRequest(packet []byte) {
	//p.core.log.Println("DEBUG: handleSwitchHopRequest")
	if p.throttle > peer_Throttle {
		return
	}
	if p.myMsg == nil {
		return
	}
	req := msgHopReq{}
	if !req.decode(packet) {
		return
	}
	if req.Root != p.myMsg.locator.root {
		return
	}
	if req.Tstamp != p.myMsg.locator.tstamp {
		return
	}
	if req.Seq != p.myMsg.seq {
		return
	}
	if uint64(len(p.myMsg.locator.coords)) <= req.Hop {
		return
	}
	res := msgHop{}
	res.Root = p.myMsg.locator.root
	res.Tstamp = p.myMsg.locator.tstamp
	res.Seq = p.myMsg.seq
	res.Hop = req.Hop
	res.Port = p.myMsg.locator.coords[res.Hop]
	sinfo := p.getSig(res.Hop)
	//p.core.log.Println("DEBUG sig:", sinfo)
	res.Next = sinfo.next
	res.Sig = sinfo.sig
	packet = res.encode()
	p.sendLinkPacket(packet)
}

func (p *peer) handleSwitchHop(packet []byte) {
	//p.core.log.Println("DEBUG: handleSwitchHop")
	if p.throttle > peer_Throttle {
		return
	}
	if p.msgAnc == nil {
		return
	}
	res := msgHop{}
	if !res.decode(packet) {
		return
	}
	if res.Root != p.msgAnc.Root {
		return
	}
	if res.Tstamp != p.msgAnc.Tstamp {
		return
	}
	if res.Seq != p.msgAnc.Seq {
		return
	}
	if res.Hop != uint64(len(p.msgHops)) {
		return
	} // always process in order
	loc := switchLocator{coords: make([]switchPort, 0, len(p.msgHops)+1)}
	loc.root = res.Root
	loc.tstamp = res.Tstamp
	for _, hop := range p.msgHops {
		loc.coords = append(loc.coords, hop.Port)
	}
	loc.coords = append(loc.coords, res.Port)
	thisHopKey := &res.Root
	if res.Hop != 0 {
		thisHopKey = &p.msgHops[res.Hop-1].Next
	}
	bs := getBytesForSig(&res.Next, &loc)
	if p.core.sigs.check(thisHopKey, &res.Sig, bs) {
		p.msgHops = append(p.msgHops, &res)
		p.processSwitchMessage()
	} else {
		p.throttle++
	}
}

func (p *peer) processSwitchMessage() {
	//p.core.log.Println("DEBUG: processSwitchMessage")
	if p.throttle > peer_Throttle {
		return
	}
	if p.msgAnc == nil {
		return
	}
	if uint64(len(p.msgHops)) < p.msgAnc.Len {
		p.requestHop(uint64(len(p.msgHops)))
		return
	}
	p.throttle++
	if p.msgAnc.Len != uint64(len(p.msgHops)) {
		return
	}
	msg := switchMessage{}
	coords := make([]switchPort, 0, len(p.msgHops))
	sigs := make([]sigInfo, 0, len(p.msgHops))
	for idx, hop := range p.msgHops {
		// Consistency checks, should be redundant (already checked these...)
		if hop.Root != p.msgAnc.Root {
			return
		}
		if hop.Tstamp != p.msgAnc.Tstamp {
			return
		}
		if hop.Seq != p.msgAnc.Seq {
			return
		}
		if hop.Hop != uint64(idx) {
			return
		}
		coords = append(coords, hop.Port)
		sigs = append(sigs, sigInfo{next: hop.Next, sig: hop.Sig})
	}
	msg.from = p.sig
	msg.locator.root = p.msgAnc.Root
	msg.locator.tstamp = p.msgAnc.Tstamp
	msg.locator.coords = coords
	msg.seq = p.msgAnc.Seq
	//msg.RSeq = p.msgAnc.RSeq
	//msg.Degree = p.msgAnc.Deg
	p.core.switchTable.handleMessage(&msg, p.port, sigs)
	if len(coords) == 0 {
		return
	}
	// Reuse locator, set the coords to the peer's coords, to use in dht
	msg.locator.coords = coords[:len(coords)-1]
	// Pass a mesage to the dht informing it that this peer (still) exists
	dinfo := dhtInfo{
		key:    p.box,
		coords: msg.locator.getCoords(),
	}
	p.core.dht.peers <- &dinfo
}

func (p *peer) sendSwitchAnnounce() {
	anc := msgAnnounce{}
	anc.Root = p.myMsg.locator.root
	anc.Tstamp = p.myMsg.locator.tstamp
	anc.Seq = p.myMsg.seq
	anc.Len = uint64(len(p.myMsg.locator.coords))
	//anc.Deg = p.myMsg.Degree
	if p.msgAnc != nil {
		anc.Rseq = p.msgAnc.Seq
	}
	packet := anc.encode()
	p.sendLinkPacket(packet)
}

func (p *peer) getSig(hop uint64) sigInfo {
	//p.core.log.Println("DEBUG getSig:", len(p.mySigs), hop)
	if hop < uint64(len(p.mySigs)) {
		return p.mySigs[hop]
	}
	bs := getBytesForSig(&p.sig, &p.myMsg.locator)
	sig := sigInfo{}
	sig.next = p.sig
	sig.sig = *sign(&p.core.sigPriv, bs)
	p.mySigs = append(p.mySigs, sig)
	//p.core.log.Println("DEBUG sig bs:", bs)
	return sig
}

func getBytesForSig(next *sigPubKey, loc *switchLocator) []byte {
	//bs, err := wire_encode_locator(loc)
	//if err != nil { panic(err) }
	bs := append([]byte(nil), next[:]...)
	bs = append(bs, wire_encode_locator(loc)...)
	//bs := wire_encode_locator(loc)
	//bs = append(next[:], bs...)
	return bs
}
