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

	"github.com/Arceliar/phony"
)

// The router struct has channels to/from the adapter device and a self peer (0), which is how messages are passed between this node and the peers/switch layer.
// The router's phony.Inbox goroutine is responsible for managing all information related to the dht, searches, and crypto sessions.
type router struct {
	phony.Inbox
	core     *Core
	addr     address.Address
	subnet   address.Subnet
	out      func([]byte) // packets we're sending to the network, link to peer's "in"
	dht      dht
	nodeinfo nodeinfo
	searches searches
	sessions sessions
	intf     routerInterface
	peer     *peer
	table    *lookupTable // has a copy of our locator
}

// Initializes the router struct, which includes setting up channels to/from the adapter.
func (r *router) init(core *Core) {
	r.core = core
	r.addr = *address.AddrForNodeID(&r.dht.nodeID)
	r.subnet = *address.SubnetForNodeID(&r.dht.nodeID)
	r.intf.router = r
	phony.Block(&r.core.peers, func() {
		// FIXME don't block here!
		r.peer = r.core.peers._newPeer(&r.core.boxPub, &r.core.sigPub, &crypto.BoxSharedKey{}, &r.intf)
	})
	r.peer.Act(r, r.peer._handleIdle)
	r.out = func(bs []byte) {
		r.peer.handlePacketFrom(r, bs)
	}
	r.nodeinfo.init(r.core)
	r.core.config.Mutex.RLock()
	r.nodeinfo.setNodeInfo(r.core.config.Current.NodeInfo, r.core.config.Current.NodeInfoPrivacy)
	r.core.config.Mutex.RUnlock()
	r.dht.init(r)
	r.searches.init(r)
	r.sessions.init(r)
}

func (r *router) updateTable(from phony.Actor, table *lookupTable) {
	r.Act(from, func() {
		r.table = table
		r.nodeinfo.Act(r, func() {
			r.nodeinfo.table = table
		})
		for _, ses := range r.sessions.sinfos {
			sinfo := ses
			sinfo.Act(r, func() {
				sinfo.table = table
			})
		}
	})
}

// Reconfigures the router and any child modules. This should only ever be run
// by the router actor.
func (r *router) reconfigure() {
	// Reconfigure the router
	current := r.core.config.GetCurrent()
	r.core.log.Println("Reloading NodeInfo...")
	if err := r.nodeinfo.setNodeInfo(current.NodeInfo, current.NodeInfoPrivacy); err != nil {
		r.core.log.Errorln("Error reloading NodeInfo:", err)
	} else {
		r.core.log.Infoln("NodeInfo updated")
	}
	// Reconfigure children
	r.dht.reconfigure()
	r.searches.reconfigure()
	r.sessions.reconfigure()
}

// Starts the tickerLoop goroutine.
func (r *router) start() error {
	r.core.log.Infoln("Starting router")
	go r.doMaintenance()
	return nil
}

// Insert a peer info into the dht, TODO? make the dht a separate actor
func (r *router) insertPeer(from phony.Actor, info *dhtInfo) {
	r.Act(from, func() {
		r.dht.insertPeer(info)
	})
}

// Reset sessions and DHT after the switch sees our coords change
func (r *router) reset(from phony.Actor) {
	r.Act(from, func() {
		r.sessions.reset()
		r.dht.reset()
	})
}

// TODO remove reconfigure so this is just a ticker loop
// and then find something better than a ticker loop to schedule things...
func (r *router) doMaintenance() {
	phony.Block(r, func() {
		// Any periodic maintenance stuff goes here
		r.core.switchTable.doMaintenance(r)
		r.dht.doMaintenance()
		r.sessions.cleanup()
	})
	time.AfterFunc(time.Second, r.doMaintenance)
}

// Checks incoming traffic type and passes it to the appropriate handler.
func (r *router) _handlePacket(packet []byte) {
	pType, pTypeLen := wire_decode_uint64(packet)
	if pTypeLen == 0 {
		return
	}
	switch pType {
	case wire_Traffic:
		r._handleTraffic(packet)
	case wire_ProtocolTraffic:
		r._handleProto(packet)
	default:
	}
}

// Handles incoming traffic, i.e. encapuslated ordinary IPv6 packets.
// Passes them to the crypto session worker to be decrypted and sent to the adapter.
func (r *router) _handleTraffic(packet []byte) {
	p := wire_trafficPacket{}
	if !p.decode(packet) {
		return
	}
	sinfo, isIn := r.sessions.getSessionForHandle(&p.Handle)
	if !isIn {
		return
	}
	sinfo.recv(r, &p)
}

// Handles protocol traffic by decrypting it, checking its type, and passing it to the appropriate handler for that traffic type.
func (r *router) _handleProto(packet []byte) {
	// First parse the packet
	p := wire_protoTrafficPacket{}
	if !p.decode(packet) {
		return
	}
	// Now try to open the payload
	var sharedKey *crypto.BoxSharedKey
	if p.ToKey == r.core.boxPub {
		// Try to open using our permanent key
		sharedKey = r.sessions.getSharedKey(&r.core.boxPriv, &p.FromKey)
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
		r._handlePing(bs, &p.FromKey)
	case wire_SessionPong:
		r._handlePong(bs, &p.FromKey)
	case wire_NodeInfoRequest:
		fallthrough
	case wire_NodeInfoResponse:
		r._handleNodeInfo(bs, &p.FromKey)
	case wire_DHTLookupRequest:
		r._handleDHTReq(bs, &p.FromKey)
	case wire_DHTLookupResponse:
		r._handleDHTRes(bs, &p.FromKey)
	default:
	}
}

// Decodes session pings from wire format and passes them to sessions.handlePing where they either create or update a session.
func (r *router) _handlePing(bs []byte, fromKey *crypto.BoxPubKey) {
	ping := sessionPing{}
	if !ping.decode(bs) {
		return
	}
	ping.SendPermPub = *fromKey
	r.sessions.handlePing(&ping)
}

// Handles session pongs (which are really pings with an extra flag to prevent acknowledgement).
func (r *router) _handlePong(bs []byte, fromKey *crypto.BoxPubKey) {
	r._handlePing(bs, fromKey)
}

// Decodes dht requests and passes them to dht.handleReq to trigger a lookup/response.
func (r *router) _handleDHTReq(bs []byte, fromKey *crypto.BoxPubKey) {
	req := dhtReq{}
	if !req.decode(bs) {
		return
	}
	req.Key = *fromKey
	r.dht.handleReq(&req)
}

// Decodes dht responses and passes them to dht.handleRes to update the DHT table and further pass them to the search code (if applicable).
func (r *router) _handleDHTRes(bs []byte, fromKey *crypto.BoxPubKey) {
	res := dhtRes{}
	if !res.decode(bs) {
		return
	}
	res.Key = *fromKey
	r.dht.handleRes(&res)
}

// Decodes nodeinfo request
func (r *router) _handleNodeInfo(bs []byte, fromKey *crypto.BoxPubKey) {
	req := nodeinfoReqRes{}
	if !req.decode(bs) {
		return
	}
	req.SendPermPub = *fromKey
	r.nodeinfo.handleNodeInfo(r, &req)
}

////////////////////////////////////////////////////////////////////////////////

// routerInterface is a helper that implements linkInterface
type routerInterface struct {
	router *router
}

func (intf *routerInterface) out(bss [][]byte) {
	// Note that this is run in the peer's goroutine
	intf.router.Act(intf.router.peer, func() {
		for _, bs := range bss {
			intf.router._handlePacket(bs)
		}
	})
	// This should now immediately make the peer idle again
	// So the self-peer shouldn't end up buffering anything
	// We let backpressure act as a throttle instead
	intf.router.peer._handleIdle()
}

func (intf *routerInterface) linkOut(_ []byte) {}

func (intf *routerInterface) close() {}

func (intf *routerInterface) name() string { return "(self)" }

func (intf *routerInterface) local() string { return "(self)" }

func (intf *routerInterface) remote() string { return "(self)" }

func (intf *routerInterface) interfaceType() string { return "self" }
