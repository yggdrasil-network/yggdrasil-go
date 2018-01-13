package yggdrasil

// This is the session manager
// It's responsible for keeping track of open sessions to other nodes
// The session information consists of crypto keys and coords

import "time"

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
	time         time.Time // Time we last received a packet
	coords       []byte    // coords of destination
	packet       []byte    // a buffered packet, sent immediately on ping/pong
	init         bool      // Reset if coords change
	send         chan []byte
	recv         chan *wire_trafficPacket
	nonceMask    uint64
	tstamp       int64 // tstamp from their last session ping, replay attack mitigation
}

// FIXME replay attacks (include nonce or some sequence number)
type sessionPing struct {
	sendPermPub boxPubKey // Sender's permanent key
	handle      handle    // Random number to ID session
	sendSesPub  boxPubKey // Session key to use
	coords      []byte
	tstamp      int64 // unix time, but the only real requirement is that it increases
	isPong      bool
}

// Returns true if the session was updated, false otherwise
func (s *sessionInfo) update(p *sessionPing) bool {
	if !(p.tstamp > s.tstamp) {
		return false
	}
	if p.sendPermPub != s.theirPermPub {
		return false
	} // Shouldn't happen
	if p.sendSesPub != s.theirSesPub {
		// FIXME need to protect against replay attacks
		//  Put a sequence number or a timestamp or something in the pings?
		// Or just return false, make the session time out?
		s.theirSesPub = p.sendSesPub
		s.theirHandle = p.handle
		s.sharedSesKey = *getSharedKey(&s.mySesPriv, &s.theirSesPub)
		s.theirNonce = boxNonce{}
		s.nonceMask = 0
	}
	s.coords = append([]byte{}, p.coords...)
	s.time = time.Now()
	s.tstamp = p.tstamp
	s.init = true
	return true
}

func (s *sessionInfo) timedout() bool {
	return time.Since(s.time) > time.Minute
}

type sessions struct {
	core *Core
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

func (ss *sessions) init(core *Core) {
	ss.core = core
	ss.permShared = make(map[boxPubKey]*boxSharedKey)
	ss.sinfos = make(map[handle]*sessionInfo)
	ss.byMySes = make(map[boxPubKey]*handle)
	ss.byTheirPerm = make(map[boxPubKey]*handle)
	ss.addrToPerm = make(map[address]*boxPubKey)
	ss.subnetToPerm = make(map[subnet]*boxPubKey)
}

func (ss *sessions) getSessionForHandle(handle *handle) (*sessionInfo, bool) {
	sinfo, isIn := ss.sinfos[*handle]
	if isIn && sinfo.timedout() {
		// We have a session, but it has timed out
		return nil, false
	}
	return sinfo, isIn
}

func (ss *sessions) getByMySes(key *boxPubKey) (*sessionInfo, bool) {
	h, isIn := ss.byMySes[*key]
	if !isIn {
		return nil, false
	}
	sinfo, isIn := ss.getSessionForHandle(h)
	return sinfo, isIn
}

func (ss *sessions) getByTheirPerm(key *boxPubKey) (*sessionInfo, bool) {
	h, isIn := ss.byTheirPerm[*key]
	if !isIn {
		return nil, false
	}
	sinfo, isIn := ss.getSessionForHandle(h)
	return sinfo, isIn
}

func (ss *sessions) getByTheirAddr(addr *address) (*sessionInfo, bool) {
	p, isIn := ss.addrToPerm[*addr]
	if !isIn {
		return nil, false
	}
	sinfo, isIn := ss.getByTheirPerm(p)
	return sinfo, isIn
}

func (ss *sessions) getByTheirSubnet(snet *subnet) (*sessionInfo, bool) {
	p, isIn := ss.subnetToPerm[*snet]
	if !isIn {
		return nil, false
	}
	sinfo, isIn := ss.getByTheirPerm(p)
	return sinfo, isIn
}

func (ss *sessions) createSession(theirPermKey *boxPubKey) *sessionInfo {
	sinfo := sessionInfo{}
	sinfo.core = ss.core
	sinfo.theirPermPub = *theirPermKey
	pub, priv := newBoxKeys()
	sinfo.mySesPub = *pub
	sinfo.mySesPriv = *priv
	sinfo.myNonce = *newBoxNonce() // TODO make sure nonceIsOK tolerates this
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
	sinfo.send = make(chan []byte, 1024)
	sinfo.recv = make(chan *wire_trafficPacket, 1024)
	go sinfo.doWorker()
	sinfo.time = time.Now()
	// Do some cleanup
	// Time thresholds almost certainly could use some adjusting
	for _, s := range ss.sinfos {
		if s.timedout() {
			s.close()
		}
	}
	ss.sinfos[sinfo.myHandle] = &sinfo
	ss.byMySes[sinfo.mySesPub] = &sinfo.myHandle
	ss.byTheirPerm[sinfo.theirPermPub] = &sinfo.myHandle
	ss.addrToPerm[sinfo.theirAddr] = &sinfo.theirPermPub
	ss.subnetToPerm[sinfo.theirSubnet] = &sinfo.theirPermPub
	return &sinfo
}

func (sinfo *sessionInfo) close() {
	delete(sinfo.core.sessions.sinfos, sinfo.myHandle)
	delete(sinfo.core.sessions.byMySes, sinfo.mySesPub)
	delete(sinfo.core.sessions.byTheirPerm, sinfo.theirPermPub)
	delete(sinfo.core.sessions.addrToPerm, sinfo.theirAddr)
	delete(sinfo.core.sessions.subnetToPerm, sinfo.theirSubnet)
	close(sinfo.send)
	close(sinfo.recv)
}

func (ss *sessions) getPing(sinfo *sessionInfo) sessionPing {
	loc := ss.core.switchTable.getLocator()
	coords := loc.getCoords()
	ref := sessionPing{
		sendPermPub: ss.core.boxPub,
		handle:      sinfo.myHandle,
		sendSesPub:  sinfo.mySesPub,
		tstamp:      time.Now().Unix(),
		coords:      coords,
	}
	sinfo.myNonce.update()
	return ref
}

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

func (ss *sessions) ping(sinfo *sessionInfo) {
	ss.sendPingPong(sinfo, false)
}

func (ss *sessions) sendPingPong(sinfo *sessionInfo, isPong bool) {
	ping := ss.getPing(sinfo)
	ping.isPong = isPong
	bs := ping.encode()
	shared := ss.getSharedKey(&ss.core.boxPriv, &sinfo.theirPermPub)
	payload, nonce := boxSeal(shared, bs, nil)
	p := wire_protoTrafficPacket{
		ttl:     ^uint64(0),
		coords:  sinfo.coords,
		toKey:   sinfo.theirPermPub,
		fromKey: ss.core.boxPub,
		nonce:   *nonce,
		payload: payload,
	}
	packet := p.encode()
	ss.core.router.out(packet)
}

func (ss *sessions) handlePing(ping *sessionPing) {
	// Get the corresponding session (or create a new session)
	sinfo, isIn := ss.getByTheirPerm(&ping.sendPermPub)
	if !isIn || sinfo.timedout() {
		if isIn {
			sinfo.close()
		}
		ss.createSession(&ping.sendPermPub)
		sinfo, isIn = ss.getByTheirPerm(&ping.sendPermPub)
		if !isIn {
			panic("This should not happen")
		}
	}
	// Update the session
	if !sinfo.update(ping) { /*panic("Should not happen in testing")*/
		return
	}
	if !ping.isPong {
		ss.sendPingPong(sinfo, true)
	}
	if sinfo.packet != nil {
		// send
		var bs []byte
		bs, sinfo.packet = sinfo.packet, nil
		go func() { sinfo.send <- bs }()
	}
}

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

func (sinfo *sessionInfo) nonceIsOK(theirNonce *boxNonce) bool {
	// The bitmask is to allow for some non-duplicate out-of-order packets
	diff := theirNonce.minus(&sinfo.theirNonce)
	if diff > 0 {
		return true
	}
	return ^sinfo.nonceMask&(0x01<<uint64(-diff)) != 0
}

func (sinfo *sessionInfo) updateNonce(theirNonce *boxNonce) {
	// Shift nonce mask if needed
	// Set bit
	diff := theirNonce.minus(&sinfo.theirNonce)
	if diff > 0 {
		sinfo.nonceMask <<= uint64(diff)
		sinfo.nonceMask &= 0x01
	} else {
		sinfo.nonceMask &= 0x01 << uint64(-diff)
	}
	sinfo.theirNonce = *theirNonce
}

func (ss *sessions) resetInits() {
	for _, sinfo := range ss.sinfos {
		sinfo.init = false
	}
}

////////////////////////////////////////////////////////////////////////////////

// This is for a per-session worker
// It handles calling the relatively expensive crypto operations
// It's also responsible for keeping nonces consistent

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

func (sinfo *sessionInfo) doSend(bs []byte) {
	defer util_putBytes(bs)
	if !sinfo.init {
		return
	} // To prevent using empty session keys
	payload, nonce := boxSeal(&sinfo.sharedSesKey, bs, &sinfo.myNonce)
	defer util_putBytes(payload)
	p := wire_trafficPacket{
		ttl:     ^uint64(0),
		coords:  sinfo.coords,
		handle:  sinfo.theirHandle,
		nonce:   *nonce,
		payload: payload,
	}
	packet := p.encode()
	sinfo.core.router.out(packet)
}

func (sinfo *sessionInfo) doRecv(p *wire_trafficPacket) {
	defer util_putBytes(p.payload)
	if !sinfo.nonceIsOK(&p.nonce) {
		return
	}
	bs, isOK := boxOpen(&sinfo.sharedSesKey, p.payload, &p.nonce)
	if !isOK {
		util_putBytes(bs)
		return
	}
	sinfo.updateNonce(&p.nonce)
	sinfo.time = time.Now()
	sinfo.core.router.recvPacket(bs, &sinfo.theirAddr)
}
