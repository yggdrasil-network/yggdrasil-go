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

// TODO?
//  Some kind of max search steps, in case the node is offline, so we don't crawl through too much of the network looking for a destination that isn't there?

import (
	"errors"
	"sort"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

// This defines the maximum number of dhtInfo that we keep track of for nodes to query in an ongoing search.
const search_MAX_SEARCH_SIZE = 16

// This defines the time after which we send a new search packet.
// Search packets are sent automatically immediately after a response is received.
// So this allows for timeouts and for long searches to become increasingly parallel.
const search_RETRY_TIME = time.Second

// Information about an ongoing search.
// Includes the target NodeID, the bitmask to match it to an IP, and the list of nodes to visit / already visited.
type searchInfo struct {
	core     *Core
	dest     crypto.NodeID
	mask     crypto.NodeID
	time     time.Time
	packet   []byte
	toVisit  []*dhtInfo
	visited  map[crypto.NodeID]bool
	callback func(*sessionInfo, error)
	// TODO context.Context for timeout and cancellation
}

// This stores a map of active searches.
type searches struct {
	core        *Core
	reconfigure chan chan error
	searches    map[crypto.NodeID]*searchInfo
}

// Initializes the searches struct.
func (s *searches) init(core *Core) {
	s.core = core
	s.reconfigure = make(chan chan error, 1)
	go func() {
		for {
			e := <-s.reconfigure
			e <- nil
		}
	}()
	s.searches = make(map[crypto.NodeID]*searchInfo)
}

// Creates a new search info, adds it to the searches struct, and returns a pointer to the info.
func (s *searches) createSearch(dest *crypto.NodeID, mask *crypto.NodeID, callback func(*sessionInfo, error)) *searchInfo {
	now := time.Now()
	//for dest, sinfo := range s.searches {
	//	if now.Sub(sinfo.time) > time.Minute {
	//		delete(s.searches, dest)
	//	}
	//}
	info := searchInfo{
		core:     s.core,
		dest:     *dest,
		mask:     *mask,
		time:     now.Add(-time.Second),
		callback: callback,
	}
	s.searches[*dest] = &info
	return &info
}

////////////////////////////////////////////////////////////////////////////////

// Checks if there's an ongoing search related to a dhtRes.
// If there is, it adds the response info to the search and triggers a new search step.
// If there's no ongoing search, or we if the dhtRes finished the search (it was from the target node), then don't do anything more.
func (sinfo *searchInfo) handleDHTRes(res *dhtRes) {
	if res == nil || sinfo.checkDHTRes(res) {
		// Either we don't recognize this search, or we just finished it
		return
	}
	// Add to the search and continue
	sinfo.addToSearch(res)
	sinfo.doSearchStep()
}

// Adds the information from a dhtRes to an ongoing search.
// Info about a node that has already been visited is not re-added to the search.
// Duplicate information about nodes toVisit is deduplicated (the newest information is kept).
// The toVisit list is sorted in ascending order of keyspace distance from the destination.
func (sinfo *searchInfo) addToSearch(res *dhtRes) {
	// Add responses to toVisit if closer to dest than the res node
	from := dhtInfo{key: res.Key, coords: res.Coords}
	sinfo.visited[*from.getNodeID()] = true
	for _, info := range res.Infos {
		if *info.getNodeID() == sinfo.core.dht.nodeID || sinfo.visited[*info.getNodeID()] {
			continue
		}
		if dht_ordered(&sinfo.dest, info.getNodeID(), from.getNodeID()) {
			// Response is closer to the destination
			sinfo.toVisit = append(sinfo.toVisit, info)
		}
	}
	// Deduplicate
	vMap := make(map[crypto.NodeID]*dhtInfo)
	for _, info := range sinfo.toVisit {
		vMap[*info.getNodeID()] = info
	}
	sinfo.toVisit = sinfo.toVisit[:0]
	for _, info := range vMap {
		sinfo.toVisit = append(sinfo.toVisit, info)
	}
	// Sort
	sort.SliceStable(sinfo.toVisit, func(i, j int) bool {
		// Should return true if i is closer to the destination than j
		return dht_ordered(&res.Dest, sinfo.toVisit[i].getNodeID(), sinfo.toVisit[j].getNodeID())
	})
	// Truncate to some maximum size
	if len(sinfo.toVisit) > search_MAX_SEARCH_SIZE {
		sinfo.toVisit = sinfo.toVisit[:search_MAX_SEARCH_SIZE]
	}
}

// If there are no nodes left toVisit, then this cleans up the search.
// Otherwise, it pops the closest node to the destination (in keyspace) off of the toVisit list and sends a dht ping.
func (sinfo *searchInfo) doSearchStep() {
	if len(sinfo.toVisit) == 0 {
		// Dead end, do cleanup
		delete(sinfo.core.searches.searches, sinfo.dest)
		sinfo.callback(nil, errors.New("search reached dead end"))
		return
	}
	// Send to the next search target
	var next *dhtInfo
	next, sinfo.toVisit = sinfo.toVisit[0], sinfo.toVisit[1:]
	rq := dhtReqKey{next.key, sinfo.dest}
	sinfo.core.dht.addCallback(&rq, sinfo.handleDHTRes)
	sinfo.core.dht.ping(next, &sinfo.dest)
}

// If we've recenty sent a ping for this search, do nothing.
// Otherwise, doSearchStep and schedule another continueSearch to happen after search_RETRY_TIME.
func (sinfo *searchInfo) continueSearch() {
	if time.Since(sinfo.time) < search_RETRY_TIME {
		return
	}
	sinfo.time = time.Now()
	sinfo.doSearchStep()
	// In case the search dies, try to spawn another thread later
	// Note that this will spawn multiple parallel searches as time passes
	// Any that die aren't restarted, but a new one will start later
	retryLater := func() {
		// FIXME this keeps the search alive forever if not for the searches map, fix that
		newSearchInfo := sinfo.core.searches.searches[sinfo.dest]
		if newSearchInfo != sinfo {
			return
		}
		sinfo.continueSearch()
	}
	go func() {
		time.Sleep(search_RETRY_TIME)
		sinfo.core.router.admin <- retryLater
	}()
}

// Calls create search, and initializes the iterative search parts of the struct before returning it.
func (s *searches) newIterSearch(dest *crypto.NodeID, mask *crypto.NodeID, callback func(*sessionInfo, error)) *searchInfo {
	sinfo := s.createSearch(dest, mask, callback)
	sinfo.toVisit = s.core.dht.lookup(dest, true)
	sinfo.visited = make(map[crypto.NodeID]bool)
	return sinfo
}

// Checks if a dhtRes is good (called by handleDHTRes).
// If the response is from the target, get/create a session, trigger a session ping, and return true.
// Otherwise return false.
func (sinfo *searchInfo) checkDHTRes(res *dhtRes) bool {
	them := crypto.GetNodeID(&res.Key)
	var destMasked crypto.NodeID
	var themMasked crypto.NodeID
	for idx := 0; idx < crypto.NodeIDLen; idx++ {
		destMasked[idx] = sinfo.dest[idx] & sinfo.mask[idx]
		themMasked[idx] = them[idx] & sinfo.mask[idx]
	}
	if themMasked != destMasked {
		return false
	}
	// They match, so create a session and send a sessionRequest
	sess, isIn := sinfo.core.sessions.getByTheirPerm(&res.Key)
	if !isIn {
		sess = sinfo.core.sessions.createSession(&res.Key)
		if sess == nil {
			// nil if the DHT search finished but the session wasn't allowed
			sinfo.callback(nil, errors.New("session not allowed"))
			return true
		}
		_, isIn := sinfo.core.sessions.getByTheirPerm(&res.Key)
		if !isIn {
			panic("This should never happen")
		}
	}
	// FIXME (!) replay attacks could mess with coords? Give it a handle (tstamp)?
	sess.coords = res.Coords
	sess.packet = sinfo.packet
	sinfo.core.sessions.ping(sess)
	sinfo.callback(sess, nil)
	// Cleanup
	delete(sinfo.core.searches.searches, res.Dest)
	return true
}
