package yggdrasil

// This part does most of the work to handle packets to/from yourself
// It also manages crypto and dht info
// TODO? move dht stuff into another goroutine?

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
}

func (r *router) init(core *Core) {
	r.core = core
	r.addr = *address_addrForNodeID(&r.core.dht.nodeID)
	in := make(chan []byte, 1024)                             // TODO something better than this...
	p := r.core.peers.newPeer(&r.core.boxPub, &r.core.sigPub) //, out, in)
	// TODO set in/out functions on the new peer...
	p.out = func(packet []byte) { in <- packet } // FIXME in theory it blocks...
	r.in = in
	// TODO? make caller responsible for go-ing if it needs to not block
	r.out = func(packet []byte) { p.handlePacket(packet, nil) }
	// TODO attach these to the tun
	//  Maybe that's the core's job...
	//  It creates tun, creates the router, creates channels, sets them?
	recv := make(chan []byte, 1024)
	send := make(chan []byte, 1024)
	r.recv = recv
	r.send = send
	r.core.tun.recv = recv
	r.core.tun.send = send
	r.reset = make(chan struct{}, 1)
	go r.mainLoop()
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
			r.core.dht.insert(info) //r.core.dht.insertIfNew(info)
		case <-r.reset:
			r.core.sessions.resetInits()
		case <-ticker.C:
			{
				// Any periodic maintenance stuff goes here
				r.core.dht.doMaintenance()
				util_getBytes() // To slowly drain things
			}
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
			sinfo = r.core.searches.createSearch(nodeID, mask)
		}
		if packet != nil {
			sinfo.packet = packet
		}
		r.core.searches.sendSearch(sinfo)
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
		// We haven't heard from the dest in a while; they may have changed coords
		// Maybe the connection is idle, or maybe one of us changed coords
		// Try searching to either ping them (a little overhead) or fix the coords
		doSearch(nil)
		fallthrough
	//default: go func() { sinfo.send<-bs }()
	default:
		sinfo.send <- bs
	}
}

func (r *router) recvPacket(bs []byte, theirAddr *address) {
	// TODO pass their NodeID, check *that* instead
	//  Or store their address in the session?...
	//fmt.Println("Recv packet")
	if theirAddr == nil {
		panic("Should not happen ever")
	}
	if len(bs) < 24 {
		return
	}
	var source address
	copy(source[:], bs[8:])
	var snet subnet
	copy(snet[:], bs[8:])
	if !source.isValid() && !snet.isValid() {
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
		return
	}
}

func (r *router) handleTraffic(packet []byte) {
	defer util_putBytes(packet)
	p := wire_trafficPacket{}
	if !p.decode(packet) {
		return
	}
	sinfo, isIn := r.core.sessions.getSessionForHandle(&p.handle)
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
	if p.toKey == r.core.boxPub {
		// Try to open using our permanent key
		sharedKey = r.core.sessions.getSharedKey(&r.core.boxPriv, &p.fromKey)
	} else {
		return
	}
	bs, isOK := boxOpen(sharedKey, p.payload, &p.nonce)
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
		r.handlePing(bs, &p.fromKey)
	case wire_SessionPong:
		r.handlePong(bs, &p.fromKey)
	case wire_DHTLookupRequest:
		r.handleDHTReq(bs, &p.fromKey)
	case wire_DHTLookupResponse:
		r.handleDHTRes(bs, &p.fromKey)
	case wire_SearchRequest:
		r.handleSearchReq(bs)
	case wire_SearchResponse:
		r.handleSearchRes(bs)
	default: /*panic("Should not happen in testing") ;*/
		return
	}
}

func (r *router) handlePing(bs []byte, fromKey *boxPubKey) {
	ping := sessionPing{}
	if !ping.decode(bs) {
		return
	}
	ping.sendPermPub = *fromKey
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
	if req.key != *fromKey {
		return
	}
	r.core.dht.handleReq(&req)
}

func (r *router) handleDHTRes(bs []byte, fromKey *boxPubKey) {
	res := dhtRes{}
	if !res.decode(bs) {
		return
	}
	if res.key != *fromKey {
		return
	}
	r.core.dht.handleRes(&res)
}

func (r *router) handleSearchReq(bs []byte) {
	req := searchReq{}
	if !req.decode(bs) {
		return
	}
	r.core.searches.handleSearchReq(&req)
}

func (r *router) handleSearchRes(bs []byte) {
	res := searchRes{}
	if !res.decode(bs) {
		return
	}
	r.core.searches.handleSearchRes(&res)
}
