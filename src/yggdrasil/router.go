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

import (
	"bytes"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

// The router struct has channels to/from the tun/tap device and a self peer (0), which is how messages are passed between this node and the peers/switch layer.
// The router's mainLoop goroutine is responsible for managing all information related to the dht, searches, and crypto sessions.
type router struct {
	core      *Core
	addr      address.Address
	subnet    address.Subnet
	in        <-chan []byte          // packets we received from the network, link to peer's "out"
	out       func([]byte)           // packets we're sending to the network, link to peer's "in"
	toRecv    chan router_recvPacket // packets to handle via recvPacket()
	tun       tunAdapter             // TUN/TAP adapter
	adapters  []Adapter              // Other adapters
	recv      chan<- []byte          // place where the tun pulls received packets from
	send      <-chan []byte          // place where the tun puts outgoing packets
	reset     chan struct{}          // signal that coords changed (re-init sessions/dht)
	admin     chan func()            // pass a lambda for the admin socket to query stuff
	cryptokey cryptokey
}

// Packet and session info, used to check that the packet matches a valid IP range or CKR prefix before sending to the tun.
type router_recvPacket struct {
	bs    []byte
	sinfo *sessionInfo
}

// Initializes the router struct, which includes setting up channels to/from the tun/tap.
func (r *router) init(core *Core) {
	r.core = core
	r.addr = *address.AddrForNodeID(&r.core.dht.nodeID)
	r.subnet = *address.SubnetForNodeID(&r.core.dht.nodeID)
	in := make(chan []byte, 32) // TODO something better than this...
	p := r.core.peers.newPeer(&r.core.boxPub, &r.core.sigPub, &crypto.BoxSharedKey{}, "(self)")
	p.out = func(packet []byte) {
		// This is to make very sure it never blocks
		select {
		case in <- packet:
			return
		default:
			util.PutBytes(packet)
		}
	}
	r.in = in
	r.out = func(packet []byte) { p.handlePacket(packet) } // The caller is responsible for go-ing if it needs to not block
	r.toRecv = make(chan router_recvPacket, 32)
	recv := make(chan []byte, 32)
	send := make(chan []byte, 32)
	r.recv = recv
	r.send = send
	r.reset = make(chan struct{}, 1)
	r.admin = make(chan func(), 32)
	r.cryptokey.init(r.core)
	r.tun.init(r.core, send, recv)
}

// Starts the mainLoop goroutine.
func (r *router) start() error {
	r.core.log.Println("Starting router")
	go r.mainLoop()
	return nil
}

// Takes traffic from the tun/tap and passes it to router.send, or from r.in and handles incoming traffic.
// Also adds new peer info to the DHT.
// Also resets the DHT and sesssions in the event of a coord change.
// Also does periodic maintenance stuff.
func (r *router) mainLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case rp := <-r.toRecv:
			r.recvPacket(rp.bs, rp.sinfo)
		case p := <-r.in:
			r.handleIn(p)
		case p := <-r.send:
			r.sendPacket(p)
		case info := <-r.core.dht.peers:
			now := time.Now()
			oldInfo, isIn := r.core.dht.table[*info.getNodeID()]
			r.core.dht.insert(info)
			if isIn && now.Sub(oldInfo.recv) < 45*time.Second {
				info.recv = oldInfo.recv
			}
		case <-r.reset:
			r.core.sessions.resetInits()
			r.core.dht.reset()
		case <-ticker.C:
			{
				// Any periodic maintenance stuff goes here
				r.core.switchTable.doMaintenance()
				r.core.dht.doMaintenance()
				r.core.sessions.cleanup()
				util.GetBytes() // To slowly drain things
			}
		case f := <-r.admin:
			f()
		}
	}
}

// Checks a packet's to/from address to make sure it's in the allowed range.
// If a session to the destination exists, gets the session and passes the packet to it.
// If no session exists, it triggers (or continues) a search.
// If the session hasn't responded recently, it triggers a ping or search to keep things alive or deal with broken coords *relatively* quickly.
// It also deals with oversized packets if there are MTU issues by calling into icmpv6.go to spoof PacketTooBig traffic, or DestinationUnreachable if the other side has their tun/tap disabled.
func (r *router) sendPacket(bs []byte) {
	var sourceAddr address.Address
	var destAddr address.Address
	var destSnet address.Subnet
	var destPubKey *crypto.BoxPubKey
	var destNodeID *crypto.NodeID
	var addrlen int
	if bs[0]&0xf0 == 0x60 {
		// Check if we have a fully-sized header
		if len(bs) < 40 {
			panic("Tried to send a packet shorter than an IPv6 header...")
		}
		// IPv6 address
		addrlen = 16
		copy(sourceAddr[:addrlen], bs[8:])
		copy(destAddr[:addrlen], bs[24:])
		copy(destSnet[:addrlen/2], bs[24:])
	} else if bs[0]&0xf0 == 0x40 {
		// Check if we have a fully-sized header
		if len(bs) < 20 {
			panic("Tried to send a packet shorter than an IPv4 header...")
		}
		// IPv4 address
		addrlen = 4
		copy(sourceAddr[:addrlen], bs[12:])
		copy(destAddr[:addrlen], bs[16:])
	} else {
		// Unknown address length
		return
	}
	if !r.cryptokey.isValidSource(sourceAddr, addrlen) {
		// The packet had a source address that doesn't belong to us or our
		// configured crypto-key routing source subnets
		return
	}
	if !destAddr.IsValid() && !destSnet.IsValid() {
		// The addresses didn't match valid Yggdrasil node addresses so let's see
		// whether it matches a crypto-key routing range instead
		if key, err := r.cryptokey.getPublicKeyForAddress(destAddr, addrlen); err == nil {
			// A public key was found, get the node ID for the search
			destPubKey = &key
			destNodeID = crypto.GetNodeID(destPubKey)
			// Do a quick check to ensure that the node ID refers to a vaild Yggdrasil
			// address or subnet - this might be superfluous
			addr := *address.AddrForNodeID(destNodeID)
			copy(destAddr[:], addr[:])
			copy(destSnet[:], addr[:])
			if !destAddr.IsValid() && !destSnet.IsValid() {
				return
			}
		} else {
			// No public key was found in the CKR table so we've exhausted our options
			return
		}
	}
	doSearch := func(packet []byte) {
		var nodeID, mask *crypto.NodeID
		switch {
		case destNodeID != nil:
			// We already know the full node ID, probably because it's from a CKR
			// route in which the public key is known ahead of time
			nodeID = destNodeID
			var m crypto.NodeID
			for i := range m {
				m[i] = 0xFF
			}
			mask = &m
		case destAddr.IsValid():
			// We don't know the full node ID - try and use the address to generate
			// a truncated node ID
			nodeID, mask = destAddr.GetNodeIDandMask()
		case destSnet.IsValid():
			// We don't know the full node ID - try and use the subnet to generate
			// a truncated node ID
			nodeID, mask = destSnet.GetNodeIDandMask()
		default:
			return
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
	if destAddr.IsValid() {
		sinfo, isIn = r.core.sessions.getByTheirAddr(&destAddr)
	}
	if destSnet.IsValid() {
		sinfo, isIn = r.core.sessions.getByTheirSubnet(&destSnet)
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
		// If we know the public key ahead of time (i.e. a CKR route) then check
		// if the session perm pub key matches before we send the packet to it
		if destPubKey != nil {
			if !bytes.Equal((*destPubKey)[:], sinfo.theirPermPub[:]) {
				return
			}
		}

		// Drop packets if the session MTU is 0 - this means that one or other
		// side probably has their TUN adapter disabled
		if sinfo.getMTU() == 0 {
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
			icmpv6Buf, err := r.tun.icmpv6.create_icmpv6_tun(
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

// Called for incoming traffic by the session worker for that connection.
// Checks that the IP address is correct (matches the session) and passes the packet to the tun/tap.
func (r *router) recvPacket(bs []byte, sinfo *sessionInfo) {
	// Note: called directly by the session worker, not the router goroutine
	if len(bs) < 24 {
		util.PutBytes(bs)
		return
	}
	var sourceAddr address.Address
	var dest address.Address
	var snet address.Subnet
	var addrlen int
	if bs[0]&0xf0 == 0x60 {
		// IPv6 address
		addrlen = 16
		copy(sourceAddr[:addrlen], bs[8:])
		copy(dest[:addrlen], bs[24:])
		copy(snet[:addrlen/2], bs[8:])
	} else if bs[0]&0xf0 == 0x40 {
		// IPv4 address
		addrlen = 4
		copy(sourceAddr[:addrlen], bs[12:])
		copy(dest[:addrlen], bs[16:])
	} else {
		// Unknown address length
		return
	}
	// Check that the packet is destined for either our Yggdrasil address or
	// subnet, or that it matches one of the crypto-key routing source routes
	if !r.cryptokey.isValidSource(dest, addrlen) {
		util.PutBytes(bs)
		return
	}
	// See whether the packet they sent should have originated from this session
	switch {
	case sourceAddr.IsValid() && sourceAddr == sinfo.theirAddr:
	case snet.IsValid() && snet == sinfo.theirSubnet:
	default:
		key, err := r.cryptokey.getPublicKeyForAddress(sourceAddr, addrlen)
		if err != nil || key != sinfo.theirPermPub {
			util.PutBytes(bs)
			return
		}
	}
	//go func() { r.recv<-bs }()
	r.recv <- bs
}

// Checks incoming traffic type and passes it to the appropriate handler.
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
	default:
	}
}

// Handles incoming traffic, i.e. encapuslated ordinary IPv6 packets.
// Passes them to the crypto session worker to be decrypted and sent to the tun/tap.
func (r *router) handleTraffic(packet []byte) {
	defer util.PutBytes(packet)
	p := wire_trafficPacket{}
	if !p.decode(packet) {
		return
	}
	sinfo, isIn := r.core.sessions.getSessionForHandle(&p.Handle)
	if !isIn {
		return
	}
	sinfo.recv <- &p
}

// Handles protocol traffic by decrypting it, checking its type, and passing it to the appropriate handler for that traffic type.
func (r *router) handleProto(packet []byte) {
	// First parse the packet
	p := wire_protoTrafficPacket{}
	if !p.decode(packet) {
		return
	}
	// Now try to open the payload
	var sharedKey *crypto.BoxSharedKey
	if p.ToKey == r.core.boxPub {
		// Try to open using our permanent key
		sharedKey = r.core.sessions.getSharedKey(&r.core.boxPriv, &p.FromKey)
	} else {
		return
	}
	bs, isOK := crypto.BoxOpen(sharedKey, p.Payload, &p.Nonce)
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
	switch bsType {
	case wire_SessionPing:
		r.handlePing(bs, &p.FromKey)
	case wire_SessionPong:
		r.handlePong(bs, &p.FromKey)
	case wire_DHTLookupRequest:
		r.handleDHTReq(bs, &p.FromKey)
	case wire_DHTLookupResponse:
		r.handleDHTRes(bs, &p.FromKey)
	default:
		util.PutBytes(packet)
	}
}

// Decodes session pings from wire format and passes them to sessions.handlePing where they either create or update a session.
func (r *router) handlePing(bs []byte, fromKey *crypto.BoxPubKey) {
	ping := sessionPing{}
	if !ping.decode(bs) {
		return
	}
	ping.SendPermPub = *fromKey
	r.core.sessions.handlePing(&ping)
}

// Handles session pongs (which are really pings with an extra flag to prevent acknowledgement).
func (r *router) handlePong(bs []byte, fromKey *crypto.BoxPubKey) {
	r.handlePing(bs, fromKey)
}

// Decodes dht requests and passes them to dht.handleReq to trigger a lookup/response.
func (r *router) handleDHTReq(bs []byte, fromKey *crypto.BoxPubKey) {
	req := dhtReq{}
	if !req.decode(bs) {
		return
	}
	req.Key = *fromKey
	r.core.dht.handleReq(&req)
}

// Decodes dht responses and passes them to dht.handleRes to update the DHT table and further pass them to the search code (if applicable).
func (r *router) handleDHTRes(bs []byte, fromKey *crypto.BoxPubKey) {
	res := dhtRes{}
	if !res.decode(bs) {
		return
	}
	res.Key = *fromKey
	r.core.dht.handleRes(&res)
}

// Passed a function to call.
// This will send the function to r.admin and block until it finishes.
// It's used by the admin socket to ask the router mainLoop goroutine about information in the session or dht structs, which cannot be read safely from outside that goroutine.
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
