package yggdrasil

// This thing manages search packets

// The basic idea is as follows:
//  We may know a NodeID (with a mask) and want to connect
//  We forward a searchReq packet through the dht
//  The last person in the dht will respond with a searchRes
//  If the responders nodeID is close enough to the requested key, it matches
//  The "close enough" is handled by a bitmask, set when the request is sent
//  For testing in the sim, it must match exactly
//  For the real world, the mask would need to map it to the desired IPv6
// This is also where we store the temporary keys used to send a request
//  Would go in sessions, but can't open one without knowing perm key
// This is largely to avoid using an iterative DHT lookup approach
//  The iterative parallel lookups from kad can skip over some DHT blackholes
//  This hides bugs, which I don't want to do right now

import "time"

//import "fmt"

type searchInfo struct {
	dest   *NodeID
	mask   *NodeID
	time   time.Time
	packet []byte
}

type searches struct {
	core     *Core
	searches map[NodeID]*searchInfo
}

func (s *searches) init(core *Core) {
	s.core = core
	s.searches = make(map[NodeID]*searchInfo)
}

func (s *searches) createSearch(dest *NodeID, mask *NodeID) *searchInfo {
	now := time.Now()
	for dest, sinfo := range s.searches {
		if now.Sub(sinfo.time) > time.Minute {
			delete(s.searches, dest)
		}
	}
	info := searchInfo{
		dest: dest,
		mask: mask,
		time: now.Add(-time.Second),
	}
	s.searches[*dest] = &info
	return &info
}

////////////////////////////////////////////////////////////////////////////////

type searchReq struct {
	key    boxPubKey // Who I am
	coords []byte    // Where I am
	dest   NodeID    // Who I'm trying to connect to
}

type searchRes struct {
	key    boxPubKey // Who I am
	coords []byte    // Where I am
	dest   NodeID    // Who I was asked about
}

func (s *searches) sendSearch(info *searchInfo) {
	now := time.Now()
	if now.Sub(info.time) < time.Second {
		return
	}
	loc := s.core.switchTable.getLocator()
	coords := loc.getCoords()
	req := searchReq{
		key:    s.core.boxPub,
		coords: coords,
		dest:   *info.dest,
	}
	info.time = time.Now()
	s.handleSearchReq(&req)
}

func (s *searches) handleSearchReq(req *searchReq) {
	lookup := s.core.dht.lookup(&req.dest, false)
	sent := false
	//fmt.Println("DEBUG len:", len(lookup))
	for _, info := range lookup {
		//fmt.Println("DEBUG lup:", info.getNodeID())
		if dht_firstCloserThanThird(info.getNodeID(),
			&req.dest,
			&s.core.dht.nodeID) {
			s.forwardSearch(req, info)
			sent = true
			break
		}
	}
	if !sent {
		s.sendSearchRes(req)
	}
}

func (s *searches) forwardSearch(req *searchReq, next *dhtInfo) {
	//fmt.Println("DEBUG fwd:", req.dest, next.getNodeID())
	bs := req.encode()
	shared := s.core.sessions.getSharedKey(&s.core.boxPriv, &next.key)
	payload, nonce := boxSeal(shared, bs, nil)
	p := wire_protoTrafficPacket{
		TTL:     ^uint64(0),
		Coords:  next.coords,
		ToKey:   next.key,
		FromKey: s.core.boxPub,
		Nonce:   *nonce,
		Payload: payload,
	}
	packet := p.encode()
	s.core.router.out(packet)
}

func (s *searches) sendSearchRes(req *searchReq) {
	//fmt.Println("DEBUG res:", req.dest, s.core.dht.nodeID)
	loc := s.core.switchTable.getLocator()
	coords := loc.getCoords()
	res := searchRes{
		key:    s.core.boxPub,
		coords: coords,
		dest:   req.dest,
	}
	bs := res.encode()
	shared := s.core.sessions.getSharedKey(&s.core.boxPriv, &req.key)
	payload, nonce := boxSeal(shared, bs, nil)
	p := wire_protoTrafficPacket{
		TTL:     ^uint64(0),
		Coords:  req.coords,
		ToKey:   req.key,
		FromKey: s.core.boxPub,
		Nonce:   *nonce,
		Payload: payload,
	}
	packet := p.encode()
	s.core.router.out(packet)
}

func (s *searches) handleSearchRes(res *searchRes) {
	info, isIn := s.searches[res.dest]
	if !isIn {
		return
	}
	them := getNodeID(&res.key)
	var destMasked NodeID
	var themMasked NodeID
	for idx := 0; idx < NodeIDLen; idx++ {
		destMasked[idx] = info.dest[idx] & info.mask[idx]
		themMasked[idx] = them[idx] & info.mask[idx]
	}
	//fmt.Println("DEBUG search res1:", themMasked, destMasked)
	//fmt.Println("DEBUG search res2:", *them, *info.dest, *info.mask)
	if themMasked != destMasked {
		return
	}
	// They match, so create a session and send a sessionRequest
	sinfo, isIn := s.core.sessions.getByTheirPerm(&res.key)
	if !isIn {
		sinfo = s.core.sessions.createSession(&res.key)
		_, isIn := s.core.sessions.getByTheirPerm(&res.key)
		if !isIn {
			panic("This should never happen")
		}
	}
	// FIXME (!) replay attacks could mess with coords? Give it a handle (tstamp)?
	sinfo.coords = res.coords
	sinfo.packet = info.packet
	s.core.sessions.ping(sinfo)
	// Cleanup
	delete(s.searches, res.dest)
}
