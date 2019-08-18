package yggdrasil

// This part does most of the work to handle packets to/from yourself
// It also manages crypto and dht info
// TODO clean up old/unused code, maybe improve comments on whatever is left

// Send:
//  Receive a packet from the adapter
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
//  The router then runs some sanity checks before passing it to the adapter

import (
	//"bytes"

	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

// The router struct has channels to/from the adapter device and a self peer (0), which is how messages are passed between this node and the peers/switch layer.
// The router's mainLoop goroutine is responsible for managing all information related to the dht, searches, and crypto sessions.
type router struct {
	core        *Core
	reconfigure chan chan error
	addr        address.Address
	subnet      address.Subnet
	in          <-chan []byte // packets we received from the network, link to peer's "out"
	out         func([]byte)  // packets we're sending to the network, link to peer's "in"
	reset       chan struct{} // signal that coords changed (re-init sessions/dht)
	admin       chan func()   // pass a lambda for the admin socket to query stuff
	nodeinfo    nodeinfo
}

// Initializes the router struct, which includes setting up channels to/from the adapter.
func (r *router) init(core *Core) {
	r.core = core
	r.reconfigure = make(chan chan error, 1)
	r.addr = *address.AddrForNodeID(&r.core.dht.nodeID)
	r.subnet = *address.SubnetForNodeID(&r.core.dht.nodeID)
	in := make(chan []byte, 1) // TODO something better than this...
	self := linkInterface{
		name: "(self)",
		info: linkInfo{
			local:    "(self)",
			remote:   "(self)",
			linkType: "self",
		},
	}
	p := r.core.peers.newPeer(&r.core.boxPub, &r.core.sigPub, &crypto.BoxSharedKey{}, &self, nil)
	p.out = func(packet []byte) { in <- packet }
	r.in = in
	out := make(chan []byte, 32)
	go func() {
		for packet := range out {
			p.handlePacket(packet)
		}
	}()
	out2 := make(chan []byte, 32)
	go func() {
		// This worker makes sure r.out never blocks
		// It will buffer traffic long enough for the switch worker to take it
		// If (somehow) you can send faster than the switch can receive, then this would use unbounded memory
		// But crypto slows sends enough that the switch should always be able to take the packets...
		var buf [][]byte
		for {
			buf = append(buf, <-out2)
			for len(buf) > 0 {
				select {
				case bs := <-out2:
					buf = append(buf, bs)
				case out <- buf[0]:
					buf = buf[1:]
				}
			}
		}
	}()
	r.out = func(packet []byte) { out2 <- packet }
	r.reset = make(chan struct{}, 1)
	r.admin = make(chan func(), 32)
	r.nodeinfo.init(r.core)
	r.core.config.Mutex.RLock()
	r.nodeinfo.setNodeInfo(r.core.config.Current.NodeInfo, r.core.config.Current.NodeInfoPrivacy)
	r.core.config.Mutex.RUnlock()
}

// Starts the mainLoop goroutine.
func (r *router) start() error {
	r.core.log.Infoln("Starting router")
	go r.mainLoop()
	return nil
}

// Takes traffic from the adapter and passes it to router.send, or from r.in and handles incoming traffic.
// Also adds new peer info to the DHT.
// Also resets the DHT and sesssions in the event of a coord change.
// Also does periodic maintenance stuff.
func (r *router) mainLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case p := <-r.in:
			r.handleIn(p)
		case info := <-r.core.dht.peers:
			r.core.dht.insertPeer(info)
		case <-r.reset:
			r.core.sessions.reset()
			r.core.dht.reset()
		case <-ticker.C:
			{
				// Any periodic maintenance stuff goes here
				r.core.switchTable.doMaintenance()
				r.core.dht.doMaintenance()
				r.core.sessions.cleanup()
			}
		case f := <-r.admin:
			f()
		case e := <-r.reconfigure:
			current := r.core.config.GetCurrent()
			e <- r.nodeinfo.setNodeInfo(current.NodeInfo, current.NodeInfoPrivacy)
		}
	}
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
// Passes them to the crypto session worker to be decrypted and sent to the adapter.
func (r *router) handleTraffic(packet []byte) {
	defer util.PutBytes(packet)
	p := wire_trafficPacket{}
	if !p.decode(packet) {
		return
	}
	sinfo, isIn := r.core.sessions.getSessionForHandle(&p.Handle)
	if !isIn {
		util.PutBytes(p.Payload)
		return
	}
	select {
	case sinfo.fromRouter <- p:
	case <-sinfo.cancel.Finished():
		util.PutBytes(p.Payload)
	}
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
	// send dht messages to dht, sessionRefresh to sessions, data to adapter...
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
	case wire_NodeInfoRequest:
		fallthrough
	case wire_NodeInfoResponse:
		r.handleNodeInfo(bs, &p.FromKey)
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

// Decodes nodeinfo request
func (r *router) handleNodeInfo(bs []byte, fromKey *crypto.BoxPubKey) {
	req := nodeinfoReqRes{}
	if !req.decode(bs) {
		return
	}
	req.SendPermPub = *fromKey
	r.nodeinfo.handleNodeInfo(&req)
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
