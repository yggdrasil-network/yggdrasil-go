package yggdrasil

// This is the session manager
// It's responsible for keeping track of open sessions to other nodes
// The session information consists of crypto keys and coords

import (
	"bytes"
	"sync"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"

	"github.com/Arceliar/phony"
)

// All the information we know about an active session.
// This includes coords, permanent and ephemeral keys, handles and nonces, various sorts of timing information for timeout and maintenance, and some metadata for the admin API.
type sessionInfo struct {
	phony.Inbox                       // Protects all of the below, use it any time you read/change the contents of a session
	sessions      *sessions           //
	theirAddr     address.Address     //
	theirSubnet   address.Subnet      //
	theirPermPub  crypto.BoxPubKey    //
	theirSesPub   crypto.BoxPubKey    //
	mySesPub      crypto.BoxPubKey    //
	mySesPriv     crypto.BoxPrivKey   //
	sharedPermKey crypto.BoxSharedKey // used for session pings
	sharedSesKey  crypto.BoxSharedKey // derived from session keys
	theirHandle   crypto.Handle       //
	myHandle      crypto.Handle       //
	theirNonce    crypto.BoxNonce     //
	myNonce       crypto.BoxNonce     //
	theirMTU      MTU                 //
	myMTU         MTU                 //
	wasMTUFixed   bool                // Was the MTU fixed by a receive error?
	timeOpened    time.Time           // Time the session was opened
	time          time.Time           // Time we last received a packet
	mtuTime       time.Time           // time myMTU was last changed
	pingTime      time.Time           // time the first ping was sent since the last received packet
	coords        []byte              // coords of destination
	reset         bool                // reset if coords change
	tstamp        int64               // ATOMIC - tstamp from their last session ping, replay attack mitigation
	bytesSent     uint64              // Bytes of real traffic sent in this session
	bytesRecvd    uint64              // Bytes of real traffic received in this session
	init          chan struct{}       // Closed when the first session pong arrives, used to signal that the session is ready for initial use
	cancel        util.Cancellation   // Used to terminate workers
	conn          *Conn               // The associated Conn object
	callbacks     []chan func()       // Finished work from crypto workers
	table         *lookupTable        // table.self is a locator where we get our coords
}

// Represents a session ping/pong packet, and includes information like public keys, a session handle, coords, a timestamp to prevent replays, and the tun/tap MTU.
type sessionPing struct {
	SendPermPub crypto.BoxPubKey // Sender's permanent key
	Handle      crypto.Handle    // Random number to ID session
	SendSesPub  crypto.BoxPubKey // Session key to use
	Coords      []byte           //
	Tstamp      int64            // unix time, but the only real requirement is that it increases
	IsPong      bool             //
	MTU         MTU              //
}

// Updates session info in response to a ping, after checking that the ping is OK.
// Returns true if the session was updated, or false otherwise.
func (s *sessionInfo) _update(p *sessionPing) bool {
	if !(p.Tstamp > s.tstamp) {
		// To protect against replay attacks
		return false
	}
	if p.SendPermPub != s.theirPermPub {
		// Should only happen if two sessions got the same handle
		// That shouldn't be allowed anyway, but if it happens then let one time out
		return false
	}
	if p.SendSesPub != s.theirSesPub {
		s.theirSesPub = p.SendSesPub
		s.theirHandle = p.Handle
		s.sharedSesKey = *crypto.GetSharedKey(&s.mySesPriv, &s.theirSesPub)
		s.theirNonce = crypto.BoxNonce{}
	}
	if p.MTU >= 1280 || p.MTU == 0 {
		s.theirMTU = p.MTU
		if s.conn != nil {
			s.conn.setMTU(s, s._getMTU())
		}
	}
	if !bytes.Equal(s.coords, p.Coords) {
		// allocate enough space for additional coords
		s.coords = append(make([]byte, 0, len(p.Coords)+11), p.Coords...)
	}
	s.time = time.Now()
	s.tstamp = p.Tstamp
	s.reset = false
	defer func() { recover() }() // Recover if the below panics
	select {
	case <-s.init:
	default:
		// Unblock anything waiting for the session to initialize
		close(s.init)
	}
	return true
}

// Struct of all active sessions.
// Sessions are indexed by handle.
// Additionally, stores maps of address/subnet onto keys, and keys onto handles.
type sessions struct {
	router           *router
	listener         *Listener
	listenerMutex    sync.Mutex
	lastCleanup      time.Time
	isAllowedHandler func(pubkey *crypto.BoxPubKey, initiator bool) bool // Returns true or false if session setup is allowed
	isAllowedMutex   sync.RWMutex                                        // Protects the above
	myMaximumMTU     MTU                                                 // Maximum allowed session MTU
	permShared       map[crypto.BoxPubKey]*crypto.BoxSharedKey           // Maps known permanent keys to their shared key, used by DHT a lot
	sinfos           map[crypto.Handle]*sessionInfo                      // Maps handle onto session info
	byTheirPerm      map[crypto.BoxPubKey]*crypto.Handle                 // Maps theirPermPub onto handle
}

// Initializes the session struct.
func (ss *sessions) init(r *router) {
	ss.router = r
	ss.permShared = make(map[crypto.BoxPubKey]*crypto.BoxSharedKey)
	ss.sinfos = make(map[crypto.Handle]*sessionInfo)
	ss.byTheirPerm = make(map[crypto.BoxPubKey]*crypto.Handle)
	ss.lastCleanup = time.Now()
	ss.myMaximumMTU = 65535
}

func (ss *sessions) reconfigure() {
	ss.router.Act(nil, func() {
		for _, session := range ss.sinfos {
			sinfo, mtu := session, ss.myMaximumMTU
			sinfo.Act(ss.router, func() {
				sinfo.myMTU = mtu
			})
			session.ping(ss.router)
		}
	})
}

// Determines whether the session with a given publickey is allowed based on
// session firewall rules.
func (ss *sessions) isSessionAllowed(pubkey *crypto.BoxPubKey, initiator bool) bool {
	ss.isAllowedMutex.RLock()
	defer ss.isAllowedMutex.RUnlock()

	if ss.isAllowedHandler == nil {
		return true
	}

	return ss.isAllowedHandler(pubkey, initiator)
}

// Gets the session corresponding to a given handle.
func (ss *sessions) getSessionForHandle(handle *crypto.Handle) (*sessionInfo, bool) {
	sinfo, isIn := ss.sinfos[*handle]
	return sinfo, isIn
}

// Gets a session corresponding to a permanent key used by the remote node.
func (ss *sessions) getByTheirPerm(key *crypto.BoxPubKey) (*sessionInfo, bool) {
	h, isIn := ss.byTheirPerm[*key]
	if !isIn {
		return nil, false
	}
	sinfo, isIn := ss.getSessionForHandle(h)
	return sinfo, isIn
}

// Creates a new session and lazily cleans up old existing sessions. This
// includes initializing session info to sane defaults (e.g. lowest supported
// MTU).
func (ss *sessions) createSession(theirPermKey *crypto.BoxPubKey) *sessionInfo {
	// TODO: this check definitely needs to be moved
	if !ss.isSessionAllowed(theirPermKey, true) {
		return nil
	}
	sinfo := sessionInfo{}
	sinfo.sessions = ss
	sinfo.theirPermPub = *theirPermKey
	sinfo.sharedPermKey = *ss.getSharedKey(&ss.router.core.boxPriv, &sinfo.theirPermPub)
	pub, priv := crypto.NewBoxKeys()
	sinfo.mySesPub = *pub
	sinfo.mySesPriv = *priv
	sinfo.myNonce = *crypto.NewBoxNonce()
	sinfo.theirMTU = 1280
	sinfo.myMTU = ss.myMaximumMTU
	now := time.Now()
	sinfo.timeOpened = now
	sinfo.time = now
	sinfo.mtuTime = now
	sinfo.pingTime = now
	sinfo.init = make(chan struct{})
	sinfo.cancel = util.NewCancellation()
	higher := false
	for idx := range ss.router.core.boxPub {
		if ss.router.core.boxPub[idx] > sinfo.theirPermPub[idx] {
			higher = true
			break
		} else if ss.router.core.boxPub[idx] < sinfo.theirPermPub[idx] {
			break
		}
	}
	if higher {
		// higher => odd nonce
		sinfo.myNonce[len(sinfo.myNonce)-1] |= 0x01
	} else {
		// lower => even nonce
		sinfo.myNonce[len(sinfo.myNonce)-1] &= 0xfe
	}
	sinfo.myHandle = *crypto.NewHandle()
	sinfo.theirAddr = *address.AddrForNodeID(crypto.GetNodeID(&sinfo.theirPermPub))
	sinfo.theirSubnet = *address.SubnetForNodeID(crypto.GetNodeID(&sinfo.theirPermPub))
	sinfo.table = ss.router.table
	ss.sinfos[sinfo.myHandle] = &sinfo
	ss.byTheirPerm[sinfo.theirPermPub] = &sinfo.myHandle
	return &sinfo
}

func (ss *sessions) cleanup() {
	// Time thresholds almost certainly could use some adjusting
	for k := range ss.permShared {
		// Delete a key, to make sure this eventually shrinks to 0
		delete(ss.permShared, k)
		break
	}
	if time.Since(ss.lastCleanup) < time.Minute {
		return
	}
	permShared := make(map[crypto.BoxPubKey]*crypto.BoxSharedKey, len(ss.permShared))
	for k, v := range ss.permShared {
		permShared[k] = v
	}
	ss.permShared = permShared
	sinfos := make(map[crypto.Handle]*sessionInfo, len(ss.sinfos))
	for k, v := range ss.sinfos {
		sinfos[k] = v
	}
	ss.sinfos = sinfos
	byTheirPerm := make(map[crypto.BoxPubKey]*crypto.Handle, len(ss.byTheirPerm))
	for k, v := range ss.byTheirPerm {
		byTheirPerm[k] = v
	}
	ss.byTheirPerm = byTheirPerm
	ss.lastCleanup = time.Now()
}

func (sinfo *sessionInfo) doRemove() {
	sinfo.sessions.router.Act(nil, func() {
		sinfo.sessions.removeSession(sinfo)
	})
}

// Closes a session, removing it from sessions maps.
func (ss *sessions) removeSession(sinfo *sessionInfo) {
	if s := sinfo.sessions.sinfos[sinfo.myHandle]; s == sinfo {
		delete(sinfo.sessions.sinfos, sinfo.myHandle)
		delete(sinfo.sessions.byTheirPerm, sinfo.theirPermPub)
	}
}

// Returns a session ping appropriate for the given session info.
func (sinfo *sessionInfo) _getPing() sessionPing {
	coords := sinfo.table.self.getCoords()
	ping := sessionPing{
		SendPermPub: sinfo.sessions.router.core.boxPub,
		Handle:      sinfo.myHandle,
		SendSesPub:  sinfo.mySesPub,
		Tstamp:      time.Now().Unix(),
		Coords:      coords,
		MTU:         sinfo.myMTU,
	}
	sinfo.myNonce.Increment()
	return ping
}

// Gets the shared key for a pair of box keys.
// Used to cache recently used shared keys for protocol traffic.
// This comes up with dht req/res and session ping/pong traffic.
func (ss *sessions) getSharedKey(myPriv *crypto.BoxPrivKey,
	theirPub *crypto.BoxPubKey) *crypto.BoxSharedKey {
	return crypto.GetSharedKey(myPriv, theirPub)
	// FIXME concurrency issues with the below, so for now we just burn the CPU every time
	if skey, isIn := ss.permShared[*theirPub]; isIn {
		return skey
	}
	// First do some cleanup
	const maxKeys = 1024
	for key := range ss.permShared {
		// Remove a random key until the store is small enough
		if len(ss.permShared) < maxKeys {
			break
		}
		delete(ss.permShared, key)
	}
	ss.permShared[*theirPub] = crypto.GetSharedKey(myPriv, theirPub)
	return ss.permShared[*theirPub]
}

// Sends a session ping by calling sendPingPong in ping mode.
func (sinfo *sessionInfo) ping(from phony.Actor) {
	sinfo.Act(from, func() {
		sinfo._sendPingPong(false)
	})
}

// Calls getPing, sets the appropriate ping/pong flag, encodes to wire format, and send it.
// Updates the time the last ping was sent in the session info.
func (sinfo *sessionInfo) _sendPingPong(isPong bool) {
	ping := sinfo._getPing()
	ping.IsPong = isPong
	bs := ping.encode()
	payload, nonce := crypto.BoxSeal(&sinfo.sharedPermKey, bs, nil)
	p := wire_protoTrafficPacket{
		Coords:  sinfo.coords,
		ToKey:   sinfo.theirPermPub,
		FromKey: sinfo.sessions.router.core.boxPub,
		Nonce:   *nonce,
		Payload: payload,
	}
	packet := p.encode()
	// TODO rewrite the below if/when the peer struct becomes an actor, to not go through the router first
	sinfo.sessions.router.Act(sinfo, func() { sinfo.sessions.router.out(packet) })
	if sinfo.pingTime.Before(sinfo.time) {
		sinfo.pingTime = time.Now()
	}
}

func (sinfo *sessionInfo) setConn(from phony.Actor, conn *Conn) {
	sinfo.Act(from, func() {
		sinfo.conn = conn
		sinfo.conn.setMTU(sinfo, sinfo._getMTU())
	})
}

// Handles a session ping, creating a session if needed and calling update, then possibly responding with a pong if the ping was in ping mode and the update was successful.
// If the session has a packet cached (common when first setting up a session), it will be sent.
func (ss *sessions) handlePing(ping *sessionPing) {
	// Get the corresponding session (or create a new session)
	sinfo, isIn := ss.getByTheirPerm(&ping.SendPermPub)
	switch {
	case ping.IsPong: // This is a response, not an initial ping, so ignore it.
	case isIn: // Session already exists
	case !ss.isSessionAllowed(&ping.SendPermPub, false): // Session is not allowed
	default:
		ss.listenerMutex.Lock()
		if ss.listener != nil {
			// This is a ping from an allowed node for which no session exists, and we have a listener ready to handle sessions.
			// We need to create a session and pass it to the listener.
			sinfo = ss.createSession(&ping.SendPermPub)
			if s, _ := ss.getByTheirPerm(&ping.SendPermPub); s != sinfo {
				panic("This should not happen")
			}
			conn := newConn(ss.router.core, crypto.GetNodeID(&sinfo.theirPermPub), &crypto.NodeID{}, sinfo)
			for i := range conn.nodeMask {
				conn.nodeMask[i] = 0xFF
			}
			sinfo.setConn(ss.router, conn)
			c := ss.listener.conn
			go func() { c <- conn }()
		}
		ss.listenerMutex.Unlock()
	}
	if sinfo != nil {
		sinfo.Act(ss.router, func() {
			// Update the session
			if !sinfo._update(ping) { /*panic("Should not happen in testing")*/
				return
			}
			if !ping.IsPong {
				sinfo._sendPingPong(true)
			}
		})
	}
}

// Get the MTU of the session.
// Will be equal to the smaller of this node's MTU or the remote node's MTU.
// If sending over links with a maximum message size (this was a thing with the old UDP code), it could be further lowered, to a minimum of 1280.
func (sinfo *sessionInfo) _getMTU() MTU {
	if sinfo.theirMTU == 0 || sinfo.myMTU == 0 {
		return 0
	}
	if sinfo.theirMTU < sinfo.myMTU {
		return sinfo.theirMTU
	}
	return sinfo.myMTU
}

// Checks if a packet's nonce is newer than any previously received
func (sinfo *sessionInfo) _nonceIsOK(theirNonce *crypto.BoxNonce) bool {
	return theirNonce.Minus(&sinfo.theirNonce) > 0
}

// Updates the nonce mask by (possibly) shifting the bitmask and setting the bit corresponding to this nonce to 1, and then updating the most recent nonce
func (sinfo *sessionInfo) _updateNonce(theirNonce *crypto.BoxNonce) {
	if theirNonce.Minus(&sinfo.theirNonce) > 0 {
		// This nonce is the newest we've seen, so make a note of that
		sinfo.theirNonce = *theirNonce
		sinfo.time = time.Now()
	}
}

// Resets all sessions to an uninitialized state.
// Called after coord changes, so attempts to use a session will trigger a new ping and notify the remote end of the coord change.
// Only call this from the router actor.
func (ss *sessions) reset() {
	for _, _sinfo := range ss.sinfos {
		sinfo := _sinfo // So we can safely put it in a closure
		sinfo.Act(ss.router, func() {
			sinfo.reset = true
		})
	}
}

////////////////////////////////////////////////////////////////////////////////
//////////////////////////// Worker Functions Below ////////////////////////////
////////////////////////////////////////////////////////////////////////////////

type sessionCryptoManager struct {
	phony.Inbox
}

func (m *sessionCryptoManager) workerGo(from phony.Actor, f func()) {
	m.Act(from, func() {
		util.WorkerGo(f)
	})
}

var manager = sessionCryptoManager{}

type FlowKeyMessage struct {
	FlowKey uint64
	Message []byte
}

func (sinfo *sessionInfo) recv(from phony.Actor, packet *wire_trafficPacket) {
	sinfo.Act(from, func() {
		sinfo._recvPacket(packet)
	})
}

func (sinfo *sessionInfo) _recvPacket(p *wire_trafficPacket) {
	select {
	case <-sinfo.init:
	default:
		return
	}
	if !sinfo._nonceIsOK(&p.Nonce) {
		return
	}
	k := sinfo.sharedSesKey
	var isOK bool
	var bs []byte
	ch := make(chan func(), 1)
	poolFunc := func() {
		bs, isOK = crypto.BoxOpen(&k, p.Payload, &p.Nonce)
		callback := func() {
			if !isOK || k != sinfo.sharedSesKey || !sinfo._nonceIsOK(&p.Nonce) {
				// Either we failed to decrypt, or the session was updated, or we
				// received this packet in the mean time
				return
			}
			sinfo._updateNonce(&p.Nonce)
			sinfo.bytesRecvd += uint64(len(bs))
			sinfo.conn.recvMsg(sinfo, bs)
		}
		ch <- callback
		sinfo.checkCallbacks()
	}
	sinfo.callbacks = append(sinfo.callbacks, ch)
	manager.workerGo(sinfo, poolFunc)
}

func (sinfo *sessionInfo) _send(msg FlowKeyMessage) {
	select {
	case <-sinfo.init:
	default:
		return
	}
	sinfo.bytesSent += uint64(len(msg.Message))
	coords := append([]byte(nil), sinfo.coords...)
	if msg.FlowKey != 0 {
		coords = append(coords, 0)
		coords = append(coords, wire_encode_uint64(msg.FlowKey)...)
	}
	p := wire_trafficPacket{
		Coords: coords,
		Handle: sinfo.theirHandle,
		Nonce:  sinfo.myNonce,
	}
	sinfo.myNonce.Increment()
	k := sinfo.sharedSesKey
	ch := make(chan func(), 1)
	poolFunc := func() {
		p.Payload, _ = crypto.BoxSeal(&k, msg.Message, &p.Nonce)
		packet := p.encode()
		callback := func() {
			sinfo.sessions.router.Act(sinfo, func() {
				sinfo.sessions.router.out(packet)
			})
		}
		ch <- callback
		sinfo.checkCallbacks()
	}
	sinfo.callbacks = append(sinfo.callbacks, ch)
	manager.workerGo(sinfo, poolFunc)
}

func (sinfo *sessionInfo) checkCallbacks() {
	sinfo.Act(nil, func() {
		if len(sinfo.callbacks) > 0 {
			select {
			case callback := <-sinfo.callbacks[0]:
				sinfo.callbacks = sinfo.callbacks[1:]
				callback()
				sinfo.checkCallbacks()
			default:
			}
		}
	})
}
