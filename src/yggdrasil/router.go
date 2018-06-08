package yggdrasil

// This part does most of the work to handle packets to/from yourself
// It also manages crypto and dht info
// TODO clean up old/unused code, maybe improve comments on whatever is left

// Send:
//  Receive a packet from the tun
//  Look up session (if none exists, trigger a search)
//  Hand off to session (which encrypts, etc)
//  Session will pass it back to router.out, which hands it off to the self peer
//  The self peer triggers a lookup to find which peer to send to next
//  And then passes it to that's peer's peer.out function
//  The peer.out function sends it over the wire to the matching peer

// Recv:
//  A packet comes in off the wire, and goes to a peer.handlePacket
//  The peer does a lookup, sees no better peer than the self
//  Hands it to the self peer.out, which passes it to router.in
//  If it's dht/seach/etc. traffic, the router passes it to that part
//  If it's an encapsulated IPv6 packet, the router looks up the session for it
//  The packet is passed to the session, which decrypts it, router.recvPacket
//  The router then runs some sanity checks before passing it to the tun

import "time"
import "golang.org/x/net/icmp"
import "golang.org/x/net/ipv6"

//import "fmt"
//import "net"

type router struct {
	core  *Core
	addr  address
	in    <-chan []byte // packets we received from the network, link to peer's "out"
	out   func([]byte)  // packets we're sending to the network, link to peer's "in"
	recv  chan<- []byte // place where the tun pulls received packets from
	send  <-chan []byte // place where the tun puts outgoing packets
	reset chan struct{} // signal that coords changed (re-init sessions/dht)
	admin chan func()   // pass a lambda for the admin socket to query stuff
}

func (r *router) init(core *Core) {
	r.core = core
	r.addr = *address_addrForNodeID(&r.core.dht.nodeID)
	in := make(chan []byte, 32) // TODO something better than this...
	p := r.core.peers.newPeer(&r.core.boxPub, &r.core.sigPub, &boxSharedKey{})
	p.out = func(packet []byte) {
		// This is to make very sure it never blocks
		select {
		case in <- packet:
			return
		default:
			util_putBytes(packet)
		}
	}
	r.in = in
	r.out = func(packet []byte) { p.handlePacket(packet) } // The caller is responsible for go-ing if it needs to not block
	recv := make(chan []byte, 32)
	send := make(chan []byte, 32)
	r.recv = recv
	r.send = send
	r.core.tun.recv = recv
	r.core.tun.send = send
	r.reset = make(chan struct{}, 1)
	r.admin = make(chan func())
	// go r.mainLoop()
}

func (r *router) start() error {
	r.core.log.Println("Starting router")
	go r.mainLoop()
	return nil
}

func (r *router) mainLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case p := <-r.in:
			r.handleIn(p)
		case p := <-r.send:
			r.sendPacket(p)
		case info := <-r.core.dht.peers:
			r.core.dht.insertIfNew(info, false) // Insert as a normal node
			r.core.dht.insertIfNew(info, true)  // Insert as a peer
		case <-r.reset:
			r.core.sessions.resetInits()
			r.core.dht.reset()
		case <-ticker.C:
			{
				// Any periodic maintenance stuff goes here
				r.core.switchTable.doMaintenance()
				r.core.dht.doMaintenance()
				//r.core.peers.sendSwitchMsgs() // FIXME debugging
				util_getBytes() // To slowly drain things
			}
		case f := <-r.admin:
			f()
		}
	}
}

func (r *router) sendPacket(bs []byte) {
	if len(bs) < 40 {
		panic("Tried to send a packet shorter than a header...")
	}
	var sourceAddr address
	var sourceSubnet subnet
	copy(sourceAddr[:], bs[8:])
	copy(sourceSubnet[:], bs[8:])
	if !sourceAddr.isValid() && !sourceSubnet.isValid() {
		return
	}
	var dest address
	copy(dest[:], bs[24:])
	var snet subnet
	copy(snet[:], bs[24:])
	if !dest.isValid() && !snet.isValid() {
		return
	}
	doSearch := func(packet []byte) {
		var nodeID, mask *NodeID
		if dest.isValid() {
			nodeID, mask = dest.getNodeIDandMask()
		}
		if snet.isValid() {
			nodeID, mask = snet.getNodeIDandMask()
		}
		sinfo, isIn := r.core.searches.searches[*nodeID]
		if !isIn {
			sinfo = r.core.searches.newIterSearch(nodeID, mask)
		}
		if packet != nil {
			sinfo.packet = packet
		}
		r.core.searches.continueSearch(sinfo)
	}
	var sinfo *sessionInfo
	var isIn bool
	if dest.isValid() {
		sinfo, isIn = r.core.sessions.getByTheirAddr(&dest)
	}
	if snet.isValid() {
		sinfo, isIn = r.core.sessions.getByTheirSubnet(&snet)
	}
	switch {
	case !isIn || !sinfo.init:
		// No or unintiialized session, so we need to search first
		doSearch(bs)
	case time.Since(sinfo.time) > 6*time.Second:
		if sinfo.time.Before(sinfo.pingTime) && time.Since(sinfo.pingTime) > 6*time.Second {
			// We haven't heard from the dest in a while
			// We tried pinging but didn't get a response
			// They may have changed coords
			// Try searching to discover new coords
			// Note that search spam is throttled internally
			doSearch(nil)
		} else {
			// We haven't heard about the dest in a while
			now := time.Now()
			if !sinfo.time.Before(sinfo.pingTime) {
				// Update pingTime to start the clock for searches (above)
				sinfo.pingTime = now
			}
			if time.Since(sinfo.pingSend) > time.Second {
				// Send at most 1 ping per second
				sinfo.pingSend = now
				r.core.sessions.sendPingPong(sinfo, false)
			}
		}
		fallthrough // Also send the packet
	default:
		// Drop packets if the session MTU is 0 - this means that one or other
		// side probably has their TUN adapter disabled
		if sinfo.getMTU() == 0 {
			// Get the size of the oversized payload, up to a max of 900 bytes
			window := 900
			if len(bs) < window {
				window = len(bs)
			}

			// Create the Destination Unreachable response
			ptb := &icmp.DstUnreach{
				Data: bs[:window],
			}

			// Create the ICMPv6 response from it
			icmpv6Buf, err := r.core.tun.icmpv6.create_icmpv6_tun(
				bs[8:24], bs[24:40],
				ipv6.ICMPTypeDestinationUnreachable, 1, ptb)
			if err == nil {
				r.recv <- icmpv6Buf
			}

			// Don't continue - drop the packet
			return
		}
		// Generate an ICMPv6 Packet Too Big for packets larger than session MTU
		if len(bs) > int(sinfo.getMTU()) {
			// Get the size of the oversized payload, up to a max of 900 bytes
			window := 900
			if int(sinfo.getMTU()) < window {
				window = int(sinfo.getMTU())
			}

			// Create the Packet Too Big response
			ptb := &icmp.PacketTooBig{
				MTU:  int(sinfo.getMTU()),
				Data: bs[:window],
			}

			// Create the ICMPv6 response from it
			icmpv6Buf, err := r.core.tun.icmpv6.create_icmpv6_tun(
				bs[8:24], bs[24:40],
				ipv6.ICMPTypePacketTooBig, 0, ptb)
			if err == nil {
				r.recv <- icmpv6Buf
			}

			// Don't continue - drop the packet
			return
		}
		sinfo.send <- bs
	}
}

func (r *router) recvPacket(bs []byte, theirAddr *address, theirSubnet *subnet) {
	// Note: called directly by the session worker, not the router goroutine
	//fmt.Println("Recv packet")
	if len(bs) < 24 {
		util_putBytes(bs)
		return
	}
	var source address
	copy(source[:], bs[8:])
	var snet subnet
	copy(snet[:], bs[8:])
	switch {
	case source.isValid() && source == *theirAddr:
	case snet.isValid() && snet == *theirSubnet:
	default:
		util_putBytes(bs)
		return
	}
	//go func() { r.recv<-bs }()
	r.recv <- bs
}

func (r *router) handleIn(packet []byte) {
	pType, pTypeLen := wire_decode_uint64(packet)
	if pTypeLen == 0 {
		return
	}
	switch pType {
	case wire_Traffic:
		r.handleTraffic(packet)
	case wire_ProtocolTraffic:
		r.handleProto(packet)
	default: /*panic("Should not happen in testing") ;*/
	}
}

func (r *router) handleTraffic(packet []byte) {
	defer util_putBytes(packet)
	p := wire_trafficPacket{}
	if !p.decode(packet) {
		return
	}
	sinfo, isIn := r.core.sessions.getSessionForHandle(&p.Handle)
	if !isIn {
		return
	}
	//go func () { sinfo.recv<-&p }()
	sinfo.recv <- &p
}

func (r *router) handleProto(packet []byte) {
	// First parse the packet
	p := wire_protoTrafficPacket{}
	if !p.decode(packet) {
		return
	}
	// Now try to open the payload
	var sharedKey *boxSharedKey
	//var theirPermPub *boxPubKey
	if p.ToKey == r.core.boxPub {
		// Try to open using our permanent key
		sharedKey = r.core.sessions.getSharedKey(&r.core.boxPriv, &p.FromKey)
	} else {
		return
	}
	bs, isOK := boxOpen(sharedKey, p.Payload, &p.Nonce)
	if !isOK {
		return
	}
	// Now do something with the bytes in bs...
	// send dht messages to dht, sessionRefresh to sessions, data to tun...
	// For data, should check that key and IP match...
	bsType, bsTypeLen := wire_decode_uint64(bs)
	if bsTypeLen == 0 {
		return
	}
	//fmt.Println("RECV bytes:", bs)
	switch bsType {
	case wire_SessionPing:
		r.handlePing(bs, &p.FromKey)
	case wire_SessionPong:
		r.handlePong(bs, &p.FromKey)
	case wire_DHTLookupRequest:
		r.handleDHTReq(bs, &p.FromKey)
	case wire_DHTLookupResponse:
		r.handleDHTRes(bs, &p.FromKey)
	default: /*panic("Should not happen in testing") ;*/
		return
	}
}

func (r *router) handlePing(bs []byte, fromKey *boxPubKey) {
	ping := sessionPing{}
	if !ping.decode(bs) {
		return
	}
	ping.SendPermPub = *fromKey
	r.core.sessions.handlePing(&ping)
}

func (r *router) handlePong(bs []byte, fromKey *boxPubKey) {
	r.handlePing(bs, fromKey)
}

func (r *router) handleDHTReq(bs []byte, fromKey *boxPubKey) {
	req := dhtReq{}
	if !req.decode(bs) {
		return
	}
	req.Key = *fromKey
	r.core.dht.handleReq(&req)
}

func (r *router) handleDHTRes(bs []byte, fromKey *boxPubKey) {
	res := dhtRes{}
	if !res.decode(bs) {
		return
	}
	res.Key = *fromKey
	r.core.dht.handleRes(&res)
}

func (r *router) doAdmin(f func()) {
	// Pass this a function that needs to be run by the router's main goroutine
	// It will pass the function to the router and wait for the router to finish
	done := make(chan struct{})
	newF := func() {
		f()
		close(done)
	}
	r.admin <- newF
	<-done
}
