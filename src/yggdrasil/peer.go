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
	out  func([]byte)
	core *Core
	port switchPort
	// This is used to limit how often we perform expensive operations
	throttle uint8 // TODO apply this sanely
	// Called when a peer is removed, to close the underlying connection, or via admin api
	close func()
	// To allow the peer to call close if idle for too long
	lastAnc time.Time // TODO? rename and use this
	// used for protocol traffic (to bypass queues)
	linkIn  (chan []byte) // handlePacket sends, linkLoop recvs
	linkOut (chan []byte)
}

const peer_Throttle = 1

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

func (p *peer) linkLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case packet, ok := <-p.linkIn:
			if !ok {
				return
			}
			p.handleLinkTraffic(packet)
		case <-ticker.C:
			if time.Since(p.lastAnc) > 16*time.Second && p.close != nil {
				// Seems to have timed out, try to trigger a close
				// FIXME this depends on lastAnc or something equivalent being updated
				p.close()
			}
			p.throttle = 0
			if p.port == 0 {
				continue
			} // Don't send announces on selfInterface
			// TODO change update logic, the new switchMsg works differently, we only need to send if something changes
			p.sendSwitchMsg()
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
		p.linkIn <- packet
	default: /*panic(pType) ;*/
		return
	}
}

func (p *peer) handleTraffic(packet []byte, pTypeLen int) {
	//if p.port != 0 && p.msgAnc == nil {
	//	// Drop traffic until the peer manages to send us at least one anc
	//  // TODO equivalent for new switch format, maybe add some bool flag?
	//	return
	//}
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
	info, sigs := p.core.switchTable.createMessage(p.port)
	var msg switchMsg
	msg.Root, msg.TStamp = info.locator.root, info.locator.tstamp
	for idx, sig := range sigs {
		hop := switchMsgHop{
			Port: info.locator.coords[idx],
			Next: sig.next,
			Sig:  sig.sig,
		}
		msg.Hops = append(msg.Hops, hop)
	}
	bs := getBytesForSig(&p.sig, &info.locator)
	msg.Hops = append(msg.Hops, switchMsgHop{
		Port: p.port,
		Next: p.sig,
		Sig:  *sign(&p.core.sigPriv, bs),
	})
	packet := msg.encode()
	//p.core.log.Println("Encoded msg:", msg, "; bytes:", packet)
	p.sendLinkPacket(packet)
}

func (p *peer) handleSwitchMsg(packet []byte) {
	var msg switchMsg
	msg.decode(packet)
	//p.core.log.Println("Decoded msg:", msg, "; bytes:", packet)
	if len(msg.Hops) < 1 {
		p.throttle++
		panic("FIXME testing")
		return
	}
	var info switchMessage
	var sigs []sigInfo
	info.locator.root = msg.Root
	info.locator.tstamp = msg.TStamp
	prevKey := msg.Root
	for _, hop := range msg.Hops {
		// Build locator and signatures
		var sig sigInfo
		sig.next = hop.Next
		sig.sig = hop.Sig
		sigs = append(sigs, sig)
		info.locator.coords = append(info.locator.coords, hop.Port)
		// Check signature
		bs := getBytesForSig(&sig.next, &info.locator)
		if !p.core.sigs.check(&prevKey, &sig.sig, bs) {
			p.throttle++
			panic("FIXME testing")
			return
		}
		prevKey = sig.next
	}
	info.from = p.sig
	info.seq = uint64(time.Now().Unix())
	p.core.switchTable.handleMessage(&info, p.port, sigs)
	// Pass a mesage to the dht informing it that this peer (still) exists
	l := info.locator
	l.coords = l.coords[:len(l.coords)-1]
	dinfo := dhtInfo{
		key:    p.box,
		coords: l.getCoords(),
	}
	p.core.dht.peers <- &dinfo
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
