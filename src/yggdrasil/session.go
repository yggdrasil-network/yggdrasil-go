package yggdrasil

// This is the session manager
// It's responsible for keeping track of open sessions to other nodes
// The session information consists of crypto keys and coords

import (
	"bytes"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

// All the information we know about an active session.
// This includes coords, permanent and ephemeral keys, handles and nonces, various sorts of timing information for timeout and maintenance, and some metadata for the admin API.
type sessionInfo struct {
	core           *Core                    //
	reconfigure    chan chan error          //
	theirAddr      address.Address          //
	theirSubnet    address.Subnet           //
	theirPermPub   crypto.BoxPubKey         //
	theirSesPub    crypto.BoxPubKey         //
	mySesPub       crypto.BoxPubKey         //
	mySesPriv      crypto.BoxPrivKey        //
	sharedSesKey   crypto.BoxSharedKey      // derived from session keys
	theirHandle    crypto.Handle            //
	myHandle       crypto.Handle            //
	theirNonce     crypto.BoxNonce          //
	theirNonceMask uint64                   //
	myNonce        crypto.BoxNonce          //
	theirMTU       uint16                   //
	myMTU          uint16                   //
	wasMTUFixed    bool                     // Was the MTU fixed by a receive error?
	time           time.Time                // Time we last received a packet
	mtuTime        time.Time                // time myMTU was last changed
	pingTime       time.Time                // time the first ping was sent since the last received packet
	pingSend       time.Time                // time the last ping was sent
	coords         []byte                   // coords of destination
	packet         []byte                   // a buffered packet, sent immediately on ping/pong
	init           bool                     // Reset if coords change
	tstamp         int64                    // ATOMIC - tstamp from their last session ping, replay attack mitigation
	bytesSent      uint64                   // Bytes of real traffic sent in this session
	bytesRecvd     uint64                   // Bytes of real traffic received in this session
	worker         chan func()              // Channel to send work to the session worker
	recv           chan *wire_trafficPacket // Received packets go here, picked up by the associated Conn
}

func (sinfo *sessionInfo) doWorker(f func()) {
	done := make(chan struct{})
	sinfo.worker <- func() {
		f()
		close(done)
	}
	<-done
}

func (sinfo *sessionInfo) workerMain() {
	for f := range sinfo.worker {
		f()
	}
}

// Represents a session ping/pong packet, andincludes information like public keys, a session handle, coords, a timestamp to prevent replays, and the tun/tap MTU.
type sessionPing struct {
	SendPermPub crypto.BoxPubKey // Sender's permanent key
	Handle      crypto.Handle    // Random number to ID session
	SendSesPub  crypto.BoxPubKey // Session key to use
	Coords      []byte           //
	Tstamp      int64            // unix time, but the only real requirement is that it increases
	IsPong      bool             //
	MTU         uint16           //
}

// Updates session info in response to a ping, after checking that the ping is OK.
// Returns true if the session was updated, or false otherwise.
func (s *sessionInfo) update(p *sessionPing) bool {
	if !(p.Tstamp > atomic.LoadInt64(&s.tstamp)) {
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
		s.theirNonceMask = 0
	}
	if p.MTU >= 1280 || p.MTU == 0 {
		s.theirMTU = p.MTU
	}
	if !bytes.Equal(s.coords, p.Coords) {
		// allocate enough space for additional coords
		s.coords = append(make([]byte, 0, len(p.Coords)+11), p.Coords...)
	}
	s.time = time.Now()
	s.tstamp = p.Tstamp
	s.init = true
	return true
}

// Struct of all active sessions.
// Sessions are indexed by handle.
// Additionally, stores maps of address/subnet onto keys, and keys onto handles.
type sessions struct {
	core          *Core
	listener      *Listener
	listenerMutex sync.Mutex
	reconfigure   chan chan error
	lastCleanup   time.Time
	permShared    map[crypto.BoxPubKey]*crypto.BoxSharedKey // Maps known permanent keys to their shared key, used by DHT a lot
	sinfos        map[crypto.Handle]*sessionInfo            // Maps (secret) handle onto session info
	conns         map[crypto.Handle]*Conn                   // Maps (secret) handle onto connections
	byMySes       map[crypto.BoxPubKey]*crypto.Handle       // Maps mySesPub onto handle
	byTheirPerm   map[crypto.BoxPubKey]*crypto.Handle       // Maps theirPermPub onto handle
	addrToPerm    map[address.Address]*crypto.BoxPubKey
	subnetToPerm  map[address.Subnet]*crypto.BoxPubKey
}

// Initializes the session struct.
func (ss *sessions) init(core *Core) {
	ss.core = core
	ss.reconfigure = make(chan chan error, 1)
	go func() {
		for {
			e := <-ss.reconfigure
			responses := make(map[crypto.Handle]chan error)
			for index, session := range ss.sinfos {
				responses[index] = make(chan error)
				session.reconfigure <- responses[index]
			}
			for _, response := range responses {
				if err := <-response; err != nil {
					e <- err
					continue
				}
			}
			e <- nil
		}
	}()
	ss.permShared = make(map[crypto.BoxPubKey]*crypto.BoxSharedKey)
	ss.sinfos = make(map[crypto.Handle]*sessionInfo)
	ss.byMySes = make(map[crypto.BoxPubKey]*crypto.Handle)
	ss.byTheirPerm = make(map[crypto.BoxPubKey]*crypto.Handle)
	ss.addrToPerm = make(map[address.Address]*crypto.BoxPubKey)
	ss.subnetToPerm = make(map[address.Subnet]*crypto.BoxPubKey)
	ss.lastCleanup = time.Now()
}

// Determines whether the session firewall is enabled.
func (ss *sessions) isSessionFirewallEnabled() bool {
	ss.core.config.Mutex.RLock()
	defer ss.core.config.Mutex.RUnlock()

	return ss.core.config.Current.SessionFirewall.Enable
}

// Determines whether the session with a given publickey is allowed based on
// session firewall rules.
func (ss *sessions) isSessionAllowed(pubkey *crypto.BoxPubKey, initiator bool) bool {
	ss.core.config.Mutex.RLock()
	defer ss.core.config.Mutex.RUnlock()

	// Allow by default if the session firewall is disabled
	if !ss.isSessionFirewallEnabled() {
		return true
	}
	// Prepare for checking whitelist/blacklist
	var box crypto.BoxPubKey
	// Reject blacklisted nodes
	for _, b := range ss.core.config.Current.SessionFirewall.BlacklistEncryptionPublicKeys {
		key, err := hex.DecodeString(b)
		if err == nil {
			copy(box[:crypto.BoxPubKeyLen], key)
			if box == *pubkey {
				return false
			}
		}
	}
	// Allow whitelisted nodes
	for _, b := range ss.core.config.Current.SessionFirewall.WhitelistEncryptionPublicKeys {
		key, err := hex.DecodeString(b)
		if err == nil {
			copy(box[:crypto.BoxPubKeyLen], key)
			if box == *pubkey {
				return true
			}
		}
	}
	// Allow outbound sessions if appropriate
	if ss.core.config.Current.SessionFirewall.AlwaysAllowOutbound {
		if initiator {
			return true
		}
	}
	// Look and see if the pubkey is that of a direct peer
	var isDirectPeer bool
	for _, peer := range ss.core.peers.ports.Load().(map[switchPort]*peer) {
		if peer.box == *pubkey {
			isDirectPeer = true
			break
		}
	}
	// Allow direct peers if appropriate
	if ss.core.config.Current.SessionFirewall.AllowFromDirect && isDirectPeer {
		return true
	}
	// Allow remote nodes if appropriate
	if ss.core.config.Current.SessionFirewall.AllowFromRemote && !isDirectPeer {
		return true
	}
	// Finally, default-deny if not matching any of the above rules
	return false
}

// Gets the session corresponding to a given handle.
func (ss *sessions) getSessionForHandle(handle *crypto.Handle) (*sessionInfo, bool) {
	sinfo, isIn := ss.sinfos[*handle]
	return sinfo, isIn
}

// Gets a session corresponding to an ephemeral session key used by this node.
func (ss *sessions) getByMySes(key *crypto.BoxPubKey) (*sessionInfo, bool) {
	h, isIn := ss.byMySes[*key]
	if !isIn {
		return nil, false
	}
	sinfo, isIn := ss.getSessionForHandle(h)
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

// Gets a session corresponding to an IPv6 address used by the remote node.
func (ss *sessions) getByTheirAddr(addr *address.Address) (*sessionInfo, bool) {
	p, isIn := ss.addrToPerm[*addr]
	if !isIn {
		return nil, false
	}
	sinfo, isIn := ss.getByTheirPerm(p)
	return sinfo, isIn
}

// Gets a session corresponding to an IPv6 /64 subnet used by the remote node/network.
func (ss *sessions) getByTheirSubnet(snet *address.Subnet) (*sessionInfo, bool) {
	p, isIn := ss.subnetToPerm[*snet]
	if !isIn {
		return nil, false
	}
	sinfo, isIn := ss.getByTheirPerm(p)
	return sinfo, isIn
}

// Creates a new session and lazily cleans up old existing sessions. This
// includse initializing session info to sane defaults (e.g. lowest supported
// MTU).
func (ss *sessions) createSession(theirPermKey *crypto.BoxPubKey) *sessionInfo {
	if !ss.isSessionAllowed(theirPermKey, true) {
		return nil
	}
	sinfo := sessionInfo{}
	sinfo.core = ss.core
	sinfo.reconfigure = make(chan chan error, 1)
	sinfo.theirPermPub = *theirPermKey
	pub, priv := crypto.NewBoxKeys()
	sinfo.mySesPub = *pub
	sinfo.mySesPriv = *priv
	sinfo.myNonce = *crypto.NewBoxNonce()
	sinfo.theirMTU = 1280
	sinfo.myMTU = 1280
	now := time.Now()
	sinfo.time = now
	sinfo.mtuTime = now
	sinfo.pingTime = now
	sinfo.pingSend = now
	higher := false
	for idx := range ss.core.boxPub {
		if ss.core.boxPub[idx] > sinfo.theirPermPub[idx] {
			higher = true
			break
		} else if ss.core.boxPub[idx] < sinfo.theirPermPub[idx] {
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
	sinfo.worker = make(chan func(), 1)
	sinfo.recv = make(chan *wire_trafficPacket, 32)
	ss.sinfos[sinfo.myHandle] = &sinfo
	ss.byMySes[sinfo.mySesPub] = &sinfo.myHandle
	ss.byTheirPerm[sinfo.theirPermPub] = &sinfo.myHandle
	ss.addrToPerm[sinfo.theirAddr] = &sinfo.theirPermPub
	ss.subnetToPerm[sinfo.theirSubnet] = &sinfo.theirPermPub
	go sinfo.workerMain()
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
	byMySes := make(map[crypto.BoxPubKey]*crypto.Handle, len(ss.byMySes))
	for k, v := range ss.byMySes {
		byMySes[k] = v
	}
	ss.byMySes = byMySes
	byTheirPerm := make(map[crypto.BoxPubKey]*crypto.Handle, len(ss.byTheirPerm))
	for k, v := range ss.byTheirPerm {
		byTheirPerm[k] = v
	}
	ss.byTheirPerm = byTheirPerm
	addrToPerm := make(map[address.Address]*crypto.BoxPubKey, len(ss.addrToPerm))
	for k, v := range ss.addrToPerm {
		addrToPerm[k] = v
	}
	ss.addrToPerm = addrToPerm
	subnetToPerm := make(map[address.Subnet]*crypto.BoxPubKey, len(ss.subnetToPerm))
	for k, v := range ss.subnetToPerm {
		subnetToPerm[k] = v
	}
	ss.subnetToPerm = subnetToPerm
	ss.lastCleanup = time.Now()
}

// Closes a session, removing it from sessions maps and killing the worker goroutine.
func (sinfo *sessionInfo) close() {
	delete(sinfo.core.sessions.sinfos, sinfo.myHandle)
	delete(sinfo.core.sessions.byMySes, sinfo.mySesPub)
	delete(sinfo.core.sessions.byTheirPerm, sinfo.theirPermPub)
	delete(sinfo.core.sessions.addrToPerm, sinfo.theirAddr)
	delete(sinfo.core.sessions.subnetToPerm, sinfo.theirSubnet)
	close(sinfo.worker)
}

// Returns a session ping appropriate for the given session info.
func (ss *sessions) getPing(sinfo *sessionInfo) sessionPing {
	loc := ss.core.switchTable.getLocator()
	coords := loc.getCoords()
	ref := sessionPing{
		SendPermPub: ss.core.boxPub,
		Handle:      sinfo.myHandle,
		SendSesPub:  sinfo.mySesPub,
		Tstamp:      time.Now().Unix(),
		Coords:      coords,
		MTU:         sinfo.myMTU,
	}
	sinfo.myNonce.Increment()
	return ref
}

// Gets the shared key for a pair of box keys.
// Used to cache recently used shared keys for protocol traffic.
// This comes up with dht req/res and session ping/pong traffic.
func (ss *sessions) getSharedKey(myPriv *crypto.BoxPrivKey,
	theirPub *crypto.BoxPubKey) *crypto.BoxSharedKey {
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
func (ss *sessions) ping(sinfo *sessionInfo) {
	ss.sendPingPong(sinfo, false)
}

// Calls getPing, sets the appropriate ping/pong flag, encodes to wire format, and send it.
// Updates the time the last ping was sent in the session info.
func (ss *sessions) sendPingPong(sinfo *sessionInfo, isPong bool) {
	ping := ss.getPing(sinfo)
	ping.IsPong = isPong
	bs := ping.encode()
	shared := ss.getSharedKey(&ss.core.boxPriv, &sinfo.theirPermPub)
	payload, nonce := crypto.BoxSeal(shared, bs, nil)
	p := wire_protoTrafficPacket{
		Coords:  sinfo.coords,
		ToKey:   sinfo.theirPermPub,
		FromKey: ss.core.boxPub,
		Nonce:   *nonce,
		Payload: payload,
	}
	packet := p.encode()
	ss.core.router.out(packet)
	if !isPong {
		sinfo.pingSend = time.Now()
	}
}

// Handles a session ping, creating a session if needed and calling update, then possibly responding with a pong if the ping was in ping mode and the update was successful.
// If the session has a packet cached (common when first setting up a session), it will be sent.
func (ss *sessions) handlePing(ping *sessionPing) {
	// Get the corresponding session (or create a new session)
	sinfo, isIn := ss.getByTheirPerm(&ping.SendPermPub)
	// Check the session firewall
	if !isIn && ss.isSessionFirewallEnabled() {
		if !ss.isSessionAllowed(&ping.SendPermPub, false) {
			return
		}
	}
	if !isIn {
		ss.createSession(&ping.SendPermPub)
		sinfo, isIn = ss.getByTheirPerm(&ping.SendPermPub)
		if !isIn {
			panic("This should not happen")
		}
		ss.listenerMutex.Lock()
		// Check and see if there's a Listener waiting to accept connections
		// TODO: this should not block if nothing is accepting
		if !ping.IsPong && ss.listener != nil {
			conn := &Conn{
				core:     ss.core,
				session:  sinfo,
				mutex:    &sync.RWMutex{},
				nodeID:   crypto.GetNodeID(&sinfo.theirPermPub),
				nodeMask: &crypto.NodeID{},
				recv:     sinfo.recv,
			}
			for i := range conn.nodeMask {
				conn.nodeMask[i] = 0xFF
			}
			ss.listener.conn <- conn
		}
		ss.listenerMutex.Unlock()
	}
	sinfo.doWorker(func() {
		// Update the session
		if !sinfo.update(ping) { /*panic("Should not happen in testing")*/
			return
		}
		if !ping.IsPong {
			ss.sendPingPong(sinfo, true)
		}
		if sinfo.packet != nil {
			/* FIXME this needs to live in the net.Conn or something, needs work in Write
			// send
			var bs []byte
			bs, sinfo.packet = sinfo.packet, nil
			ss.core.router.sendPacket(bs) // FIXME this needs to live in the net.Conn or something, needs work in Write
			*/
			sinfo.packet = nil
		}
	})
}

// Get the MTU of the session.
// Will be equal to the smaller of this node's MTU or the remote node's MTU.
// If sending over links with a maximum message size (this was a thing with the old UDP code), it could be further lowered, to a minimum of 1280.
func (sinfo *sessionInfo) getMTU() uint16 {
	if sinfo.theirMTU == 0 || sinfo.myMTU == 0 {
		return 0
	}
	if sinfo.theirMTU < sinfo.myMTU {
		return sinfo.theirMTU
	}
	return sinfo.myMTU
}

// Checks if a packet's nonce is recent enough to fall within the window of allowed packets, and not already received.
func (sinfo *sessionInfo) nonceIsOK(theirNonce *crypto.BoxNonce) bool {
	// The bitmask is to allow for some non-duplicate out-of-order packets
	diff := theirNonce.Minus(&sinfo.theirNonce)
	if diff > 0 {
		return true
	}
	return ^sinfo.theirNonceMask&(0x01<<uint64(-diff)) != 0
}

// Updates the nonce mask by (possibly) shifting the bitmask and setting the bit corresponding to this nonce to 1, and then updating the most recent nonce
func (sinfo *sessionInfo) updateNonce(theirNonce *crypto.BoxNonce) {
	// Shift nonce mask if needed
	// Set bit
	diff := theirNonce.Minus(&sinfo.theirNonce)
	if diff > 0 {
		// This nonce is newer, so shift the window before setting the bit, and update theirNonce in the session info.
		sinfo.theirNonceMask <<= uint64(diff)
		sinfo.theirNonceMask &= 0x01
		sinfo.theirNonce = *theirNonce
	} else {
		// This nonce is older, so set the bit but do not shift the window.
		sinfo.theirNonceMask &= 0x01 << uint64(-diff)
	}
}

// Resets all sessions to an uninitialized state.
// Called after coord changes, so attemtps to use a session will trigger a new ping and notify the remote end of the coord change.
func (ss *sessions) resetInits() {
	for _, sinfo := range ss.sinfos {
		sinfo.doWorker(func() {
			sinfo.init = false
		})
	}
}
