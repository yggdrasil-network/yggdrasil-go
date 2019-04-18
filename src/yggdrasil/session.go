package yggdrasil

// This is the session manager
// It's responsible for keeping track of open sessions to other nodes
// The session information consists of crypto keys and coords

import (
	"bytes"
	"encoding/hex"
	"sync"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

// All the information we know about an active session.
// This includes coords, permanent and ephemeral keys, handles and nonces, various sorts of timing information for timeout and maintenance, and some metadata for the admin API.
type sessionInfo struct {
	core            *Core
	reconfigure     chan chan error
	theirAddr       address.Address
	theirSubnet     address.Subnet
	theirPermPub    crypto.BoxPubKey
	theirSesPub     crypto.BoxPubKey
	mySesPub        crypto.BoxPubKey
	mySesPriv       crypto.BoxPrivKey
	sharedSesKey    crypto.BoxSharedKey // derived from session keys
	theirHandle     crypto.Handle
	myHandle        crypto.Handle
	theirNonce      crypto.BoxNonce
	theirNonceMutex sync.RWMutex // protects the above
	myNonce         crypto.BoxNonce
	myNonceMutex    sync.RWMutex // protects the above
	theirMTU        uint16
	myMTU           uint16
	wasMTUFixed     bool      // Was the MTU fixed by a receive error?
	time            time.Time // Time we last received a packet
	coords          []byte    // coords of destination
	packet          []byte    // a buffered packet, sent immediately on ping/pong
	init            bool      // Reset if coords change
	send            chan []byte
	recv            chan *wire_trafficPacket
	nonceMask       uint64
	tstamp          int64     // tstamp from their last session ping, replay attack mitigation
	tstampMutex     int64     // protects the above
	mtuTime         time.Time // time myMTU was last changed
	pingTime        time.Time // time the first ping was sent since the last received packet
	pingSend        time.Time // time the last ping was sent
	bytesSent       uint64    // Bytes of real traffic sent in this session
	bytesRecvd      uint64    // Bytes of real traffic received in this session
}

// Represents a session ping/pong packet, andincludes information like public keys, a session handle, coords, a timestamp to prevent replays, and the tun/tap MTU.
type sessionPing struct {
	SendPermPub crypto.BoxPubKey // Sender's permanent key
	Handle      crypto.Handle    // Random number to ID session
	SendSesPub  crypto.BoxPubKey // Session key to use
	Coords      []byte
	Tstamp      int64 // unix time, but the only real requirement is that it increases
	IsPong      bool
	MTU         uint16
}

// Updates session info in response to a ping, after checking that the ping is OK.
// Returns true if the session was updated, or false otherwise.
func (s *sessionInfo) update(p *sessionPing) bool {
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
		s.nonceMask = 0
	}
	if p.MTU >= 1280 || p.MTU == 0 {
		s.theirMTU = p.MTU
	}
	if !bytes.Equal(s.coords, p.Coords) {
		// allocate enough space for additional coords
		s.coords = append(make([]byte, 0, len(p.Coords)+11), p.Coords...)
	}
	now := time.Now()
	s.time = now
	s.tstamp = p.Tstamp
	s.init = true
	return true
}

// Returns true if the session has been idle for longer than the allowed timeout.
func (s *sessionInfo) timedout() bool {
	return time.Since(s.time) > time.Minute
}

// Struct of all active sessions.
// Sessions are indexed by handle.
// Additionally, stores maps of address/subnet onto keys, and keys onto handles.
type sessions struct {
	core         *Core
	reconfigure  chan chan error
	lastCleanup  time.Time
	permShared   map[crypto.BoxPubKey]*crypto.BoxSharedKey // Maps known permanent keys to their shared key, used by DHT a lot
	sinfos       map[crypto.Handle]*sessionInfo            // Maps (secret) handle onto session info
	conns        map[crypto.Handle]*Conn                   // Maps (secret) handle onto connections
	byMySes      map[crypto.BoxPubKey]*crypto.Handle       // Maps mySesPub onto handle
	byTheirPerm  map[crypto.BoxPubKey]*crypto.Handle       // Maps theirPermPub onto handle
	addrToPerm   map[address.Address]*crypto.BoxPubKey
	subnetToPerm map[address.Subnet]*crypto.BoxPubKey
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
	if isIn && sinfo.timedout() {
		// We have a session, but it has timed out
		return nil, false
	}
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

// Creates a new session and lazily cleans up old/timedout existing sessions.
// This includse initializing session info to sane defaults (e.g. lowest supported MTU).
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
	if ss.core.router.adapter != nil {
		sinfo.myMTU = uint16(ss.core.router.adapter.MTU())
	}
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
	sinfo.send = make(chan []byte, 32)
	sinfo.recv = make(chan *wire_trafficPacket, 32)
	go sinfo.doWorker()
	ss.sinfos[sinfo.myHandle] = &sinfo
	ss.byMySes[sinfo.mySesPub] = &sinfo.myHandle
	ss.byTheirPerm[sinfo.theirPermPub] = &sinfo.myHandle
	ss.addrToPerm[sinfo.theirAddr] = &sinfo.theirPermPub
	ss.subnetToPerm[sinfo.theirSubnet] = &sinfo.theirPermPub
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
	for _, s := range ss.sinfos {
		if s.timedout() {
			s.close()
		}
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
	close(sinfo.send)
	close(sinfo.recv)
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
	if !isIn || sinfo.timedout() {
		if isIn {
			sinfo.close()
		}
		ss.createSession(&ping.SendPermPub)
		sinfo, isIn = ss.getByTheirPerm(&ping.SendPermPub)
		if !isIn {
			panic("This should not happen")
		}
	}
	// Update the session
	if !sinfo.update(ping) { /*panic("Should not happen in testing")*/
		return
	}
	if !ping.IsPong {
		ss.sendPingPong(sinfo, true)
	}
	if sinfo.packet != nil {
		// send
		var bs []byte
		bs, sinfo.packet = sinfo.packet, nil
		ss.core.router.sendPacket(bs)
	}
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
	return ^sinfo.nonceMask&(0x01<<uint64(-diff)) != 0
}

// Updates the nonce mask by (possibly) shifting the bitmask and setting the bit corresponding to this nonce to 1, and then updating the most recent nonce
func (sinfo *sessionInfo) updateNonce(theirNonce *crypto.BoxNonce) {
	// Shift nonce mask if needed
	// Set bit
	diff := theirNonce.Minus(&sinfo.theirNonce)
	if diff > 0 {
		// This nonce is newer, so shift the window before setting the bit, and update theirNonce in the session info.
		sinfo.nonceMask <<= uint64(diff)
		sinfo.nonceMask &= 0x01
		sinfo.theirNonce = *theirNonce
	} else {
		// This nonce is older, so set the bit but do not shift the window.
		sinfo.nonceMask &= 0x01 << uint64(-diff)
	}
}

// Resets all sessions to an uninitialized state.
// Called after coord changes, so attemtps to use a session will trigger a new ping and notify the remote end of the coord change.
func (ss *sessions) resetInits() {
	for _, sinfo := range ss.sinfos {
		sinfo.init = false
	}
}

////////////////////////////////////////////////////////////////////////////////
/*
// This is for a per-session worker.
// It handles calling the relatively expensive crypto operations.
// It's also responsible for checking nonces and dropping out-of-date/duplicate packets, or else calling the function to update nonces if the packet is OK.
func (sinfo *sessionInfo) doWorker() {
	send := make(chan []byte, 32)
	defer close(send)
	go func() {
		for bs := range send {
			sinfo.doSend(bs)
		}
	}()
	recv := make(chan *wire_trafficPacket, 32)
	defer close(recv)
	go func() {
		for p := range recv {
			sinfo.doRecv(p)
		}
	}()
	for {
		select {
		case p, ok := <-sinfo.recv:
			if ok {
				select {
				case recv <- p:
				default:
					// We need something to not block, and it's best to drop it before we decrypt
					util.PutBytes(p.Payload)
				}
			} else {
				return
			}
		case bs, ok := <-sinfo.send:
			if ok {
				send <- bs
			} else {
				return
			}
		case e := <-sinfo.reconfigure:
			e <- nil
		}
	}
}

// This encrypts a packet, creates a trafficPacket struct, encodes it, and sends it to router.out to pass it to the switch layer.
func (sinfo *sessionInfo) doSend(bs []byte) {
	defer util.PutBytes(bs)
	if !sinfo.init {
		// To prevent using empty session keys
		return
	}
	// code isn't multithreaded so appending to this is safe
	coords := sinfo.coords
	// Work out the flowkey - this is used to determine which switch queue
	// traffic will be pushed to in the event of congestion
	var flowkey uint64
	// Get the IP protocol version from the packet
	switch bs[0] & 0xf0 {
	case 0x40: // IPv4 packet
		// Check the packet meets minimum UDP packet length
		if len(bs) >= 24 {
			// Is the protocol TCP, UDP or SCTP?
			if bs[9] == 0x06 || bs[9] == 0x11 || bs[9] == 0x84 {
				ihl := bs[0] & 0x0f * 4 // Header length
				flowkey = uint64(bs[9])<<32 /* proto */ |
					uint64(bs[ihl+0])<<24 | uint64(bs[ihl+1])<<16 /* sport */ |
					uint64(bs[ihl+2])<<8 | uint64(bs[ihl+3]) /* dport */
			}
		}
	case 0x60: // IPv6 packet
		// Check if the flowlabel was specified in the packet header
		flowkey = uint64(bs[1]&0x0f)<<16 | uint64(bs[2])<<8 | uint64(bs[3])
		// If the flowlabel isn't present, make protokey from proto | sport | dport
		// if the packet meets minimum UDP packet length
		if flowkey == 0 && len(bs) >= 48 {
			// Is the protocol TCP, UDP or SCTP?
			if bs[6] == 0x06 || bs[6] == 0x11 || bs[6] == 0x84 {
				flowkey = uint64(bs[6])<<32 /* proto */ |
					uint64(bs[40])<<24 | uint64(bs[41])<<16 /* sport */ |
					uint64(bs[42])<<8 | uint64(bs[43]) /* dport */
			}
		}
	}
	// If we have a flowkey, either through the IPv6 flowlabel field or through
	// known TCP/UDP/SCTP proto-sport-dport triplet, then append it to the coords.
	// Appending extra coords after a 0 ensures that we still target the local router
	// but lets us send extra data (which is otherwise ignored) to help separate
	// traffic streams into independent queues
	if flowkey != 0 {
		coords = append(coords, 0)                // First target the local switchport
		coords = wire_put_uint64(flowkey, coords) // Then variable-length encoded flowkey
	}
	// Prepare the payload
	payload, nonce := crypto.BoxSeal(&sinfo.sharedSesKey, bs, &sinfo.myNonce)
	defer util.PutBytes(payload)
	p := wire_trafficPacket{
		Coords:  coords,
		Handle:  sinfo.theirHandle,
		Nonce:   *nonce,
		Payload: payload,
	}
	packet := p.encode()
	sinfo.bytesSent += uint64(len(bs))
	sinfo.core.router.out(packet)
}

// This takes a trafficPacket and checks the nonce.
// If the nonce is OK, it decrypts the packet.
// If the decrypted packet is OK, it calls router.recvPacket to pass the packet to the tun/tap.
// If a packet does not decrypt successfully, it assumes the packet was truncated, and updates the MTU accordingly.
// TODO? remove the MTU updating part? That should never happen with TCP peers, and the old UDP code that caused it was removed (and if replaced, should be replaced with something that can reliably send messages with an arbitrary size).
func (sinfo *sessionInfo) doRecv(p *wire_trafficPacket) {
	defer util.PutBytes(p.Payload)
	if !sinfo.nonceIsOK(&p.Nonce) {
		return
	}
	bs, isOK := crypto.BoxOpen(&sinfo.sharedSesKey, p.Payload, &p.Nonce)
	if !isOK {
		util.PutBytes(bs)
		return
	}
	sinfo.updateNonce(&p.Nonce)
	sinfo.time = time.Now()
	sinfo.bytesRecvd += uint64(len(bs))
	sinfo.core.router.toRecv <- router_recvPacket{bs, sinfo}
}
