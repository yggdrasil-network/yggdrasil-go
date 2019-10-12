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
	searches *searches
	dest     crypto.NodeID
	mask     crypto.NodeID
	time     time.Time
	toVisit  []*dhtInfo
	visited  map[crypto.NodeID]bool
	callback func(*sessionInfo, error)
	// TODO context.Context for timeout and cancellation
}

// This stores a map of active searches.
type searches struct {
	router   *router
	searches map[crypto.NodeID]*searchInfo
}

// Initializes the searches struct.
func (s *searches) init(r *router) {
	s.router = r
	s.searches = make(map[crypto.NodeID]*searchInfo)
}

func (s *searches) reconfigure() {
	// This is where reconfiguration would go, if we had anything to do
}

// Creates a new search info, adds it to the searches struct, and returns a pointer to the info.
func (s *searches) createSearch(dest *crypto.NodeID, mask *crypto.NodeID, callback func(*sessionInfo, error)) *searchInfo {
	info := searchInfo{
		searches: s,
		dest:     *dest,
		mask:     *mask,
		time:     time.Now(),
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
		if *info.getNodeID() == sinfo.searches.router.dht.nodeID || sinfo.visited[*info.getNodeID()] {
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
		if time.Since(sinfo.time) > search_RETRY_TIME {
			// Dead end and no response in too long, do cleanup
			delete(sinfo.searches.searches, sinfo.dest)
			sinfo.callback(nil, errors.New("search reached dead end"))
		}
		return
	}
	// Send to the next search target
	var next *dhtInfo
	next, sinfo.toVisit = sinfo.toVisit[0], sinfo.toVisit[1:]
	rq := dhtReqKey{next.key, sinfo.dest}
	sinfo.searches.router.dht.addCallback(&rq, sinfo.handleDHTRes)
	sinfo.searches.router.dht.ping(next, &sinfo.dest)
	sinfo.time = time.Now()
}

// If we've recenty sent a ping for this search, do nothing.
// Otherwise, doSearchStep and schedule another continueSearch to happen after search_RETRY_TIME.
func (sinfo *searchInfo) continueSearch() {
	sinfo.doSearchStep()
	// In case the search dies, try to spawn another thread later
	// Note that this will spawn multiple parallel searches as time passes
	// Any that die aren't restarted, but a new one will start later
	time.AfterFunc(search_RETRY_TIME, func() {
		sinfo.searches.router.Act(nil, func() {
			// FIXME this keeps the search alive forever if not for the searches map, fix that
			newSearchInfo := sinfo.searches.searches[sinfo.dest]
			if newSearchInfo != sinfo {
				return
			}
			sinfo.continueSearch()
		})
	})
}

// Calls create search, and initializes the iterative search parts of the struct before returning it.
func (s *searches) newIterSearch(dest *crypto.NodeID, mask *crypto.NodeID, callback func(*sessionInfo, error)) *searchInfo {
	sinfo := s.createSearch(dest, mask, callback)
	sinfo.visited = make(map[crypto.NodeID]bool)
	loc := s.router.core.switchTable.getLocator()
	sinfo.toVisit = append(sinfo.toVisit, &dhtInfo{
		key:    s.router.core.boxPub,
		coords: loc.getCoords(),
	}) // Start the search by asking ourself, useful if we're the destination
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
	sess, isIn := sinfo.searches.router.sessions.getByTheirPerm(&res.Key)
	if !isIn {
		sess = sinfo.searches.router.sessions.createSession(&res.Key)
		if sess == nil {
			// nil if the DHT search finished but the session wasn't allowed
			sinfo.callback(nil, errors.New("session not allowed"))
			// Cleanup
			delete(sinfo.searches.searches, res.Dest)
			return true
		}
		_, isIn := sinfo.searches.router.sessions.getByTheirPerm(&res.Key)
		if !isIn {
			panic("This should never happen")
		}
	} else {
		sess.coords = res.Coords         // In case coords have updated
		sess.ping(sinfo.searches.router) // In case the remote side needs updating
		sinfo.callback(nil, errors.New("session already exists"))
		// Cleanup
		delete(sinfo.searches.searches, res.Dest)
		return true
	}
	// FIXME (!) replay attacks could mess with coords? Give it a handle (tstamp)?
	sess.coords = res.Coords
	sess.ping(sinfo.searches.router)
	sinfo.callback(sess, nil)
	// Cleanup
	delete(sinfo.searches.searches, res.Dest)
	return true
}
