package yggdrasil

// This thing manages search packets

// The basic idea is as follows:
//  We may know a NodeID (with a mask) and want to connect
//  We begin a search by initializing a list of all nodes in our DHT, sorted by closest to the destination
//  We then iteratively ping nodes from the search, marking each pinged node as visited
//  We add any unvisited nodes from ping responses to the search, truncating to some maximum search size
//  This stops when we either run out of nodes to ping (we hit a dead end where we can't make progress without going back), or we reach the destination
//  A new search packet is sent immediately after receiving a response
//  A new search packet is sent periodically, once per second, in case a packet was dropped (this slowly causes the search to become parallel if the search doesn't timeout but also doesn't finish within 1 second for whatever reason)

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
	sinfo.toVisit = s.core.dht.lookup(dest, true)
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

