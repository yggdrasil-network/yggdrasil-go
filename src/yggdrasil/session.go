package yggdrasil

// This is the session manager
// It's responsible for keeping track of open sessions to other nodes
// The session information consists of crypto keys and coords

import "time"

// All the information we know about an active session.
// This includes coords, permanent and ephemeral keys, handles and nonces, various sorts of timing information for timeout and maintenance, and some metadata for the admin API.
type sessionInfo struct {
	core         *Core
	theirAddr    address
	theirSubnet  subnet
	theirPermPub boxPubKey
	theirSesPub  boxPubKey
	mySesPub     boxPubKey
	mySesPriv    boxPrivKey
	sharedSesKey boxSharedKey // derived from session keys
	theirHandle  handle
	myHandle     handle
	theirNonce   boxNonce
	myNonce      boxNonce
	theirMTU     uint16
	myMTU        uint16
	wasMTUFixed  bool      // Was the MTU fixed by a receive error?
	time         time.Time // Time we last received a packet
	coords       []byte    // coords of destination
	packet       []byte    // a buffered packet, sent immediately on ping/pong
	init         bool      // Reset if coords change
	send         chan []byte
	recv         chan *wire_trafficPacket
	nonceMask    uint64
	tstamp       int64     // tstamp from their last session ping, replay attack mitigation
	mtuTime      time.Time // time myMTU was last changed
	pingTime     time.Time // time the first ping was sent since the last received packet
	pingSend     time.Time // time the last ping was sent
	bytesSent    uint64    // Bytes of real traffic sent in this session
	bytesRecvd   uint64    // Bytes of real traffic received in this session
}

// Represents a session ping/pong packet, andincludes information like public keys, a session handle, coords, a timestamp to prevent replays, and the tun/tap MTU.
type sessionPing struct {
	SendPermPub boxPubKey // Sender's permanent key
	Handle      handle    // Random number to ID session
	SendSesPub  boxPubKey // Session key to use
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
		s.sharedSesKey = *getSharedKey(&s.mySesPriv, &s.theirSesPub)
		s.theirNonce = boxNonce{}
		s.nonceMask = 0
	}
	if p.MTU >= 1280 || p.MTU == 0 {
		s.theirMTU = p.MTU
	}
	s.coords = append([]byte{}, p.Coords...)
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
	core        *Core
	lastCleanup time.Time
	// Maps known permanent keys to their shared key, used by DHT a lot
	permShared map[boxPubKey]*boxSharedKey
	// Maps (secret) handle onto session info
	sinfos map[handle]*sessionInfo
	// Maps mySesPub onto handle
	byMySes map[boxPubKey]*handle
	// Maps theirPermPub onto handle
	byTheirPerm  map[boxPubKey]*handle
	addrToPerm   map[address]*boxPubKey
	subnetToPerm map[subnet]*boxPubKey
}

// Initializes the session struct.
func (ss *sessions) init(core *Core) {
	ss.core = core
	ss.permShared = make(map[boxPubKey]*boxSharedKey)
	ss.sinfos = make(map[handle]*sessionInfo)
	ss.byMySes = make(map[boxPubKey]*handle)
	ss.byTheirPerm = make(map[boxPubKey]*handle)
	ss.addrToPerm = make(map[address]*boxPubKey)
	ss.subnetToPerm = make(map[subnet]*boxPubKey)
	ss.lastCleanup = time.Now()
}

// Gets the session corresponding to a given handle.
func (ss *sessions) getSessionForHandle(handle *handle) (*sessionInfo, bool) {
	sinfo, isIn := ss.sinfos[*handle]
	if isIn && sinfo.timedout() {
		// We have a session, but it has timed out
		return nil, false
	}
	return sinfo, isIn
}

// Gets a session corresponding to an ephemeral session key used by this node.
func (ss *sessions) getByMySes(key *boxPubKey) (*sessionInfo, bool) {
	h, isIn := ss.byMySes[*key]
	if !isIn {
		return nil, false
	}
	sinfo, isIn := ss.getSessionForHandle(h)
	return sinfo, isIn
}

// Gets a session corresponding to a permanent key used by the remote node.
func (ss *sessions) getByTheirPerm(key *boxPubKey) (*sessionInfo, bool) {
	h, isIn := ss.byTheirPerm[*key]
	if !isIn {
		return nil, false
	}
	sinfo, isIn := ss.getSessionForHandle(h)
	return sinfo, isIn
}

// Gets a session corresponding to an IPv6 address used by the remote node.
func (ss *sessions) getByTheirAddr(addr *address) (*sessionInfo, bool) {
	p, isIn := ss.addrToPerm[*addr]
	if !isIn {
		return nil, false
	}
	sinfo, isIn := ss.getByTheirPerm(p)
	return sinfo, isIn
}

// Gets a session corresponding to an IPv6 /64 subnet used by the remote node/network.
func (ss *sessions) getByTheirSubnet(snet *subnet) (*sessionInfo, bool) {
	p, isIn := ss.subnetToPerm[*snet]
	if !isIn {
		return nil, false
	}
	sinfo, isIn := ss.getByTheirPerm(p)
	return sinfo, isIn
}

// Creates a new session and lazily cleans up old/timedout existing sessions.
// This includse initializing session info to sane defaults (e.g. lowest supported MTU).
func (ss *sessions) createSession(theirPermKey *boxPubKey) *sessionInfo {
	sinfo := sessionInfo{}
	sinfo.core = ss.core
	sinfo.theirPermPub = *theirPermKey
	pub, priv := newBoxKeys()
	sinfo.mySesPub = *pub
	sinfo.mySesPriv = *priv
	sinfo.myNonce = *newBoxNonce()
	sinfo.theirMTU = 1280
	sinfo.myMTU = uint16(ss.core.tun.mtu)
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
	sinfo.myHandle = *newHandle()
	sinfo.theirAddr = *address_addrForNodeID(getNodeID(&sinfo.theirPermPub))
	sinfo.theirSubnet = *address_subnetForNodeID(getNodeID(&sinfo.theirPermPub))
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
	if time.Since(ss.lastCleanup) < time.Minute {
		return
	}
	for _, s := range ss.sinfos {
		if s.timedout() {
			s.close()
		}
	}
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
	sinfo.myNonce.update()
	return ref
}

// Gets the shared key for a pair of box keys.
// Used to cache recently used shared keys for protocol traffic.
// This comes up with dht req/res and session ping/pong traffic.
func (ss *sessions) getSharedKey(myPriv *boxPrivKey,
	theirPub *boxPubKey) *boxSharedKey {
	if skey, isIn := ss.permShared[*theirPub]; isIn {
		return skey
	}
	// First do some cleanup
	const maxKeys = dht_bucket_number * dht_bucket_size
	for key := range ss.permShared {
		// Remove a random key until the store is small enough
		if len(ss.permShared) < maxKeys {
			break
		}
		delete(ss.permShared, key)
	}
	ss.permShared[*theirPub] = getSharedKey(myPriv, theirPub)
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
	payload, nonce := boxSeal(shared, bs, nil)
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

// Used to subtract one nonce from another, staying in the range +- 64.
// This is used by the nonce progression machinery to advance the bitmask of recently received packets (indexed by nonce), or to check the appropriate bit of the bitmask.
// It's basically part of the machinery that prevents replays and duplicate packets.
func (n *boxNonce) minus(m *boxNonce) int64 {
	diff := int64(0)
	for idx := range n {
		diff *= 256
		diff += int64(n[idx]) - int64(m[idx])
		if diff > 64 {
			diff = 64
		}
		if diff < -64 {
			diff = -64
		}
	}
	return diff
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
func (sinfo *sessionInfo) nonceIsOK(theirNonce *boxNonce) bool {
	// The bitmask is to allow for some non-duplicate out-of-order packets
	diff := theirNonce.minus(&sinfo.theirNonce)
	if diff > 0 {
		return true
	}
	return ^sinfo.nonceMask&(0x01<<uint64(-diff)) != 0
}

// Updates the nonce mask by (possibly) shifting the bitmask and setting the bit corresponding to this nonce to 1, and then updating the most recent nonce
func (sinfo *sessionInfo) updateNonce(theirNonce *boxNonce) {
	// Shift nonce mask if needed
	// Set bit
	diff := theirNonce.minus(&sinfo.theirNonce)
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

// This is for a per-session worker.
// It handles calling the relatively expensive crypto operations.
// It's also responsible for checking nonces and dropping out-of-date/duplicate packets, or else calling the function to update nonces if the packet is OK.
func (sinfo *sessionInfo) doWorker() {
	for {
		select {
		case p, ok := <-sinfo.recv:
			if ok {
				sinfo.doRecv(p)
			} else {
				return
			}
		case bs, ok := <-sinfo.send:
			if ok {
				sinfo.doSend(bs)
			} else {
				return
			}
		}
	}
}

// This encrypts a packet, creates a trafficPacket struct, encodes it, and sends it to router.out to pass it to the switch layer.
func (sinfo *sessionInfo) doSend(bs []byte) {
	defer util_putBytes(bs)
	if !sinfo.init {
		return
	} // To prevent using empty session keys

	// Read IPv6 flowlabel field (20 bits).
	// Assumes packet at least contains IPv6 header.
	flowkey := uint64(bs[1]&0x0f)<<16 | uint64(bs[2])<<8 | uint64(bs[3])
	if flowkey == 0 /* not specified */ &&
		len(bs) >= 48 /* min UDP len, others are bigger */ &&
		(bs[6] == 0x06 || bs[6] == 0x11 || bs[6] == 0x84) /* TCP UDP SCTP */ {
		// if flowlabel was unspecified (0), try to use known protocols' ports
		// protokey: proto | sport | dport
		flowkey = uint64(bs[6])<<32 /* proto */ |
			uint64(bs[40])<<24 | uint64(bs[41])<<16 /* sport */ |
			uint64(bs[42])<<8 | uint64(bs[43]) /* dport */
	}
	var coords []byte
	if flowkey != 0 {
		// Now we append something to the coords
		// Specifically, we append a 0, and then arbitrary data
		// The 0 ensures that the destination node switch forwards to the self peer (router)
		// The rest is ignored, but it's still part as the coords, so it affects switch queues
		// This helps separate traffic streams (coords, flowlabel) to be queued independently

		// TODO could we avoid allocations there and put this work into wire_trafficPacket.encode()?
		coords = append(coords, sinfo.coords...)  // Start with the real coords
		coords = append(coords, 0)                // Then target the local switchport
		coords = wire_put_uint64(flowkey, coords) // Then variable-length encoded flowkey
	} else {
		// flowlabel was unspecified (0) and protocol unrecognised.
		// To save bytes, we're not including it, therefore we won't need self-port override either.
		// So just use sinfo.coords directly to avoid golang GC allocations.
		// Recent enough Linux and BSDs support flowlabels (auto_flowlabel) out of the box so this will be rare.
		coords = sinfo.coords
	}
	payload, nonce := boxSeal(&sinfo.sharedSesKey, bs, &sinfo.myNonce)
	defer util_putBytes(payload)
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
	defer util_putBytes(p.Payload)
	if !sinfo.nonceIsOK(&p.Nonce) {
		return
	}
	bs, isOK := boxOpen(&sinfo.sharedSesKey, p.Payload, &p.Nonce)
	if !isOK {
		util_putBytes(bs)
		return
	}
	sinfo.updateNonce(&p.Nonce)
	sinfo.time = time.Now()
	sinfo.bytesRecvd += uint64(len(bs))
	sinfo.core.router.recvPacket(bs, &sinfo.theirAddr, &sinfo.theirSubnet)
}
