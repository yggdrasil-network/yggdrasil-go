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
import "math"

//import "fmt"

type peers struct {
	core        *Core
	authBoxPubs map[boxPubKey]struct{}
	mutex       sync.Mutex   // Synchronize writes to atomic
	ports       atomic.Value //map[Port]*peer, use CoW semantics
	//ports map[Port]*peer
}

func (ps *peers) init(c *Core) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	ps.putPorts(make(map[switchPort]*peer))
	ps.core = c
	ps.authBoxPubs = make(map[boxPubKey]struct{})
}

func (ps *peers) isAuthBoxPub(box *boxPubKey) bool {
	_, isIn := ps.authBoxPubs[*box]
	return isIn || len(ps.authBoxPubs) == 0
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
	bandwidth uint64
	// BUG: sync/atomic, 32 bit platforms need the above to be the first element
	box    boxPubKey
	sig    sigPubKey
	shared boxSharedKey
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

func (p *peer) getBandwidth() float64 {
	bits := atomic.LoadUint64(&p.bandwidth)
	return math.Float64frombits(bits)
}

func (p *peer) updateBandwidth(bytes int, duration time.Duration) {
	if p == nil {
		return
	}
	for ok := false; !ok; {
		oldBits := atomic.LoadUint64(&p.bandwidth)
		oldBandwidth := math.Float64frombits(oldBits)
		bandwidth := oldBandwidth*7/8 + float64(bytes)/duration.Seconds()
		bits := math.Float64bits(bandwidth)
		ok = atomic.CompareAndSwapUint64(&p.bandwidth, oldBits, bits)
	}
}

func (ps *peers) newPeer(box *boxPubKey,
	sig *sigPubKey) *peer {
	p := peer{box: *box,
		sig:     *sig,
		shared:  *getSharedKey(&ps.core.boxPriv, box),
		lastAnc: time.Now(),
		core:    ps.core}
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
			case lastRSeq != p.msgAnc.seq:
				update = true
			case p.msgAnc.rseq != p.myMsg.seq:
				update = true
			case counter%4 == 0:
				update = true
			}
			if update {
				if p.msgAnc != nil {
					lastRSeq = p.msgAnc.seq
				}
				p.sendSwitchAnnounce()
			}
			counter = (counter + 1) % 4
		}
	}
}

func (p *peer) handlePacket(packet []byte, linkIn chan<- []byte) {
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
	if p.msgAnc == nil {
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
}

func (p *peer) sendLinkPacket(packet []byte) {
	bs, nonce := boxSeal(&p.shared, packet, nil)
	linkPacket := wire_linkProtoTrafficPacket{
		nonce:   *nonce,
		payload: bs,
	}
	packet = linkPacket.encode()
	p.sendPacket(packet)
}

func (p *peer) handleLinkTraffic(bs []byte) {
	packet := wire_linkProtoTrafficPacket{}
	if !packet.decode(bs) {
		return
	}
	payload, isOK := boxOpen(&p.shared, packet.payload, &packet.nonce)
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
		anc.root != p.msgAnc.root ||
		anc.tstamp != p.msgAnc.tstamp ||
		anc.seq != p.msgAnc.seq {
		p.msgHops = nil
	}
	p.msgAnc = &anc
	p.processSwitchMessage()
	p.lastAnc = time.Now()
}

func (p *peer) requestHop(hop uint64) {
	//p.core.log.Println("DEBUG requestHop")
	req := msgHopReq{}
	req.root = p.msgAnc.root
	req.tstamp = p.msgAnc.tstamp
	req.seq = p.msgAnc.seq
	req.hop = hop
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
	if req.root != p.myMsg.locator.root {
		return
	}
	if req.tstamp != p.myMsg.locator.tstamp {
		return
	}
	if req.seq != p.myMsg.seq {
		return
	}
	if uint64(len(p.myMsg.locator.coords)) <= req.hop {
		return
	}
	res := msgHop{}
	res.root = p.myMsg.locator.root
	res.tstamp = p.myMsg.locator.tstamp
	res.seq = p.myMsg.seq
	res.hop = req.hop
	res.port = p.myMsg.locator.coords[res.hop]
	sinfo := p.getSig(res.hop)
	//p.core.log.Println("DEBUG sig:", sinfo)
	res.next = sinfo.next
	res.sig = sinfo.sig
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
	if res.root != p.msgAnc.root {
		return
	}
	if res.tstamp != p.msgAnc.tstamp {
		return
	}
	if res.seq != p.msgAnc.seq {
		return
	}
	if res.hop != uint64(len(p.msgHops)) {
		return
	} // always process in order
	loc := switchLocator{coords: make([]switchPort, 0, len(p.msgHops)+1)}
	loc.root = res.root
	loc.tstamp = res.tstamp
	for _, hop := range p.msgHops {
		loc.coords = append(loc.coords, hop.port)
	}
	loc.coords = append(loc.coords, res.port)
	thisHopKey := &res.root
	if res.hop != 0 {
		thisHopKey = &p.msgHops[res.hop-1].next
	}
	bs := getBytesForSig(&res.next, &loc)
	if p.core.sigs.check(thisHopKey, &res.sig, bs) {
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
	if uint64(len(p.msgHops)) < p.msgAnc.len {
		p.requestHop(uint64(len(p.msgHops)))
		return
	}
	p.throttle++
	if p.msgAnc.len != uint64(len(p.msgHops)) {
		return
	}
	msg := switchMessage{}
	coords := make([]switchPort, 0, len(p.msgHops))
	sigs := make([]sigInfo, 0, len(p.msgHops))
	for idx, hop := range p.msgHops {
		// Consistency checks, should be redundant (already checked these...)
		if hop.root != p.msgAnc.root {
			return
		}
		if hop.tstamp != p.msgAnc.tstamp {
			return
		}
		if hop.seq != p.msgAnc.seq {
			return
		}
		if hop.hop != uint64(idx) {
			return
		}
		coords = append(coords, hop.port)
		sigs = append(sigs, sigInfo{next: hop.next, sig: hop.sig})
	}
	msg.from = p.sig
	msg.locator.root = p.msgAnc.root
	msg.locator.tstamp = p.msgAnc.tstamp
	msg.locator.coords = coords
	msg.seq = p.msgAnc.seq
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
	anc.root = p.myMsg.locator.root
	anc.tstamp = p.myMsg.locator.tstamp
	anc.seq = p.myMsg.seq
	anc.len = uint64(len(p.myMsg.locator.coords))
	//anc.Deg = p.myMsg.Degree
	if p.msgAnc != nil {
		anc.rseq = p.msgAnc.seq
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
