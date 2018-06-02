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

import "sort"
import "time"

//import "fmt"

const search_MAX_SEARCH_SIZE = 16
const search_RETRY_TIME = time.Second

type searchInfo struct {
	dest    NodeID
	mask    NodeID
	time    time.Time
	packet  []byte
	toVisit []*dhtInfo
	visited map[NodeID]bool
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
		dest: *dest,
		mask: *mask,
		time: now.Add(-time.Second),
	}
	s.searches[*dest] = &info
	return &info
}

////////////////////////////////////////////////////////////////////////////////

func (s *searches) handleDHTRes(res *dhtRes) {
	sinfo, isIn := s.searches[res.Dest]
	if !isIn || s.checkDHTRes(sinfo, res) {
		// Either we don't recognize this search, or we just finished it
		return
	} else {
		// Add to the search and continue
		s.addToSearch(sinfo, res)
		s.doSearchStep(sinfo)
	}
}

func (s *searches) addToSearch(sinfo *searchInfo, res *dhtRes) {
	// Add responses to toVisit if closer to dest than the res node
	from := dhtInfo{key: res.Key, coords: res.Coords}
	for _, info := range res.Infos {
		if sinfo.visited[*info.getNodeID()] {
			continue
		}
		if dht_firstCloserThanThird(info.getNodeID(), &res.Dest, from.getNodeID()) {
			sinfo.toVisit = append(sinfo.toVisit, info)
		}
	}
	// Deduplicate
	vMap := make(map[NodeID]*dhtInfo)
	for _, info := range sinfo.toVisit {
		vMap[*info.getNodeID()] = info
	}
	sinfo.toVisit = sinfo.toVisit[:0]
	for _, info := range vMap {
		sinfo.toVisit = append(sinfo.toVisit, info)
	}
	// Sort
	sort.SliceStable(sinfo.toVisit, func(i, j int) bool {
		return dht_firstCloserThanThird(sinfo.toVisit[i].getNodeID(), &res.Dest, sinfo.toVisit[j].getNodeID())
	})
	// Truncate to some maximum size
	if len(sinfo.toVisit) > search_MAX_SEARCH_SIZE {
		sinfo.toVisit = sinfo.toVisit[:search_MAX_SEARCH_SIZE]
	}
}

func (s *searches) doSearchStep(sinfo *searchInfo) {
	if len(sinfo.toVisit) == 0 {
		// Dead end, do cleanup
		delete(s.searches, sinfo.dest)
		return
	} else {
		// Send to the next search target
		var next *dhtInfo
		next, sinfo.toVisit = sinfo.toVisit[0], sinfo.toVisit[1:]
		s.core.dht.ping(next, &sinfo.dest)
		sinfo.visited[*next.getNodeID()] = true
	}
}

func (s *searches) continueSearch(sinfo *searchInfo) {
	if time.Since(sinfo.time) < search_RETRY_TIME {
		return
	}
	sinfo.time = time.Now()
	s.doSearchStep(sinfo)
	// In case the search dies, try to spawn another thread later
	// Note that this will spawn multiple parallel searches as time passes
	// Any that die aren't restarted, but a new one will start later
	retryLater := func() {
		newSearchInfo := s.searches[sinfo.dest]
		if newSearchInfo != sinfo {
			return
		}
		s.continueSearch(sinfo)
	}
	go func() {
		time.Sleep(search_RETRY_TIME)
		s.core.router.admin <- retryLater
	}()
}

func (s *searches) newIterSearch(dest *NodeID, mask *NodeID) *searchInfo {
	sinfo := s.createSearch(dest, mask)
	sinfo.toVisit = s.core.dht.lookup(dest, false)
	sinfo.visited = make(map[NodeID]bool)
	return sinfo
}

func (s *searches) checkDHTRes(info *searchInfo, res *dhtRes) bool {
	them := getNodeID(&res.Key)
	var destMasked NodeID
	var themMasked NodeID
	for idx := 0; idx < NodeIDLen; idx++ {
		destMasked[idx] = info.dest[idx] & info.mask[idx]
		themMasked[idx] = them[idx] & info.mask[idx]
	}
	if themMasked != destMasked {
		return false
	}
	// They match, so create a session and send a sessionRequest
	sinfo, isIn := s.core.sessions.getByTheirPerm(&res.Key)
	if !isIn {
		sinfo = s.core.sessions.createSession(&res.Key)
		_, isIn := s.core.sessions.getByTheirPerm(&res.Key)
		if !isIn {
			panic("This should never happen")
		}
	}
	// FIXME (!) replay attacks could mess with coords? Give it a handle (tstamp)?
	sinfo.coords = res.Coords
	sinfo.packet = info.packet
	s.core.sessions.ping(sinfo)
	// Cleanup
	delete(s.searches, res.Dest)
	return true
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
		dest:   info.dest,
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
