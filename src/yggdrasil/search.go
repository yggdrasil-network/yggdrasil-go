package yggdrasil

// This thing manages search packets

// The basic idea is as follows:
//  We may know a NodeID (with a mask) and want to connect
//  We begin a search by sending a dht lookup to ourself
//  Each time a node responds, we sort the results and filter to only include useful nodes
//  We then periodically send a packet to the first node from the list (after re-filtering)
//  This happens in parallel for each node that replies
//  Meanwhile, we keep a list of the (up to) 16 closest nodes to the destination that we've visited
//  We only consider an unvisited node useful if either the list isn't full or the unvisited node is closer to the destination than the furthest node on the list
//  That gives the search some chance to recover if it hits a dead end where a node doesn't know everyone it should

import (
	"errors"
	"sort"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

// This defines the time after which we time out a search (so it can restart).
const search_RETRY_TIME = 3 * time.Second
const search_STEP_TIME = time.Second
const search_MAX_RESULTS = dht_lookup_size

// Information about an ongoing search.
// Includes the target NodeID, the bitmask to match it to an IP, and the list of nodes to visit / already visited.
type searchInfo struct {
	searches *searches
	dest     crypto.NodeID
	mask     crypto.NodeID
	time     time.Time
	visited  []*crypto.NodeID // Closest addresses visited so far
	callback func(*sessionInfo, error)
	// TODO context.Context for timeout and cancellation
	send uint64 // log number of requests sent
	recv uint64 // log number of responses received
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
	if nfo := sinfo.searches.searches[sinfo.dest]; nfo != sinfo {
		return // already done
	}
	if res != nil {
		sinfo.recv++
		if sinfo.checkDHTRes(res) {
			return // Search finished successfully
		}
		// Use results to start an additional search thread
		infos := append([]*dhtInfo(nil), res.Infos...)
		infos = sinfo.getAllowedInfos(infos)
		if len(infos) > 0 {
			sinfo.continueSearch(infos)
		}
	}
}

// If there has been no response in too long, then this cleans up the search.
// Otherwise, it pops the closest node to the destination (in keyspace) off of the toVisit list and sends a dht ping.
func (sinfo *searchInfo) doSearchStep(infos []*dhtInfo) {
	if len(infos) > 0 {
		// Send to the next search target
		next := infos[0]
		rq := dhtReqKey{next.key, sinfo.dest}
		sinfo.searches.router.dht.addCallback(&rq, sinfo.handleDHTRes)
		sinfo.searches.router.dht.ping(next, &sinfo.dest)
		sinfo.send++
	}
}

// Get a list of search targets that are close enough to the destination to try
// Requires an initial list as input
func (sinfo *searchInfo) getAllowedInfos(infos []*dhtInfo) []*dhtInfo {
	var temp []*dhtInfo
	for _, info := range infos {
		if false && len(sinfo.visited) < search_MAX_RESULTS {
			// We're not full on results yet, so don't block anything yet
		} else if !dht_ordered(&sinfo.dest, info.getNodeID(), sinfo.visited[len(sinfo.visited)-1]) {
			// Too far away
			continue
		}
		var known bool
		for _, nfo := range sinfo.visited {
			if *nfo == *info.getNodeID() {
				known = true
				break
			}
		}
		if !known {
			temp = append(temp, info)
		}
	}
	infos = append(infos[:0], temp...) // restrict to only the allowed infos
	sort.SliceStable(infos, func(i, j int) bool {
		// Should return true if i is closer to the destination than j
		return dht_ordered(&sinfo.dest, infos[i].getNodeID(), infos[j].getNodeID())
	}) // Sort infos to start with the closest
	if len(infos) > search_MAX_RESULTS {
		infos = infos[:search_MAX_RESULTS] // Limit max number of infos
	}
	return infos
}

// Run doSearchStep and schedule another continueSearch to happen after search_RETRY_TIME.
// Must not be called with an empty list of infos
func (sinfo *searchInfo) continueSearch(infos []*dhtInfo) {
	sinfo.doSearchStep(infos)
	infos = infos[1:] // Remove the node we just tried
	// In case there's no response, try the next node in infos later
	time.AfterFunc(search_STEP_TIME, func() {
		sinfo.searches.router.Act(nil, func() {
			// FIXME this keeps the search alive forever if not for the searches map, fix that
			newSearchInfo := sinfo.searches.searches[sinfo.dest]
			if newSearchInfo != sinfo {
				return
			}
			// Get good infos here instead of at the top, to make sure we can always start things off with a continueSearch call to ourself
			infos = sinfo.getAllowedInfos(infos)
			if len(infos) > 0 {
				sinfo.continueSearch(infos)
			}
		})
	})
}

// Initially start a search
func (sinfo *searchInfo) startSearch() {
	loc := sinfo.searches.router.core.switchTable.getLocator()
	var infos []*dhtInfo
	infos = append(infos, &dhtInfo{
		key:    sinfo.searches.router.core.boxPub,
		coords: loc.getCoords(),
	})
	// Start the search by asking ourself, useful if we're the destination
	sinfo.continueSearch(infos)
	// Start a timer to clean up the search if everything times out
	var cleanupFunc func()
	cleanupFunc = func() {
		sinfo.searches.router.Act(nil, func() {
			// FIXME this keeps the search alive forever if not for the searches map, fix that
			newSearchInfo := sinfo.searches.searches[sinfo.dest]
			if newSearchInfo != sinfo {
				return
			}
			elapsed := time.Since(sinfo.time)
			if elapsed > search_RETRY_TIME {
				// cleanup
				delete(sinfo.searches.searches, sinfo.dest)
				sinfo.searches.router.core.log.Debugln("search timeout:", &sinfo.dest, sinfo.send, sinfo.recv)
				sinfo.callback(nil, errors.New("search reached dead end"))
				return
			}
			time.AfterFunc(search_RETRY_TIME-elapsed, cleanupFunc)
		})
	}
	time.AfterFunc(search_RETRY_TIME, cleanupFunc)
}

// Calls create search, and initializes the iterative search parts of the struct before returning it.
func (s *searches) newIterSearch(dest *crypto.NodeID, mask *crypto.NodeID, callback func(*sessionInfo, error)) *searchInfo {
	sinfo := s.createSearch(dest, mask, callback)
	sinfo.visited = append(sinfo.visited, &s.router.dht.nodeID)
	return sinfo
}

// Checks if a dhtRes is good (called by handleDHTRes).
// If the response is from the target, get/create a session, trigger a session ping, and return true.
// Otherwise return false.
func (sinfo *searchInfo) checkDHTRes(res *dhtRes) bool {
	from := dhtInfo{key: res.Key, coords: res.Coords}
	them := from.getNodeID()
	var known bool
	for _, v := range sinfo.visited {
		if *v == *them {
			known = true
			break
		}
	}
	if !known {
		if len(sinfo.visited) < search_MAX_RESULTS || dht_ordered(&sinfo.dest, them, sinfo.visited[len(sinfo.visited)-1]) {
			// Closer to the destination than the threshold, so update visited
			sinfo.searches.router.core.log.Debugln("Updating search:", &sinfo.dest, them, sinfo.send, sinfo.recv)
			sinfo.visited = append(sinfo.visited, them)
			sort.SliceStable(sinfo.visited, func(i, j int) bool {
				// Should return true if i is closer to the destination than j
				return dht_ordered(&sinfo.dest, sinfo.visited[i], sinfo.visited[j])
			}) // Sort infos to start with the closest
			if len(sinfo.visited) > search_MAX_RESULTS {
				sinfo.visited = sinfo.visited[:search_MAX_RESULTS]
			}
			sinfo.time = time.Now()
		}
	}
	var destMasked crypto.NodeID
	var themMasked crypto.NodeID
	for idx := 0; idx < crypto.NodeIDLen; idx++ {
		destMasked[idx] = sinfo.dest[idx] & sinfo.mask[idx]
		themMasked[idx] = them[idx] & sinfo.mask[idx]
	}
	if themMasked != destMasked {
		return false
	}
	finishSearch := func(sess *sessionInfo, err error) {
		if sess != nil {
			// FIXME (!) replay attacks could mess with coords? Give it a handle (tstamp)?
			sess.Act(sinfo.searches.router, func() { sess.coords = res.Coords })
			sess.ping(sinfo.searches.router)
		}
		if err != nil {
			sinfo.callback(nil, err)
		} else {
			sinfo.callback(sess, nil)
		}
		// Cleanup
		if _, isIn := sinfo.searches.searches[sinfo.dest]; isIn {
			sinfo.searches.router.core.log.Debugln("Finished search:", &sinfo.dest, sinfo.send, sinfo.recv)
			delete(sinfo.searches.searches, res.Dest)
		}
	}
	// They match, so create a session and send a sessionRequest
	var err error
	sess, isIn := sinfo.searches.router.sessions.getByTheirPerm(&res.Key)
	if !isIn {
		// Don't already have a session
		sess = sinfo.searches.router.sessions.createSession(&res.Key)
		if sess == nil {
			err = errors.New("session not allowed")
		} else if _, isIn := sinfo.searches.router.sessions.getByTheirPerm(&res.Key); !isIn {
			panic("This should never happen")
		}
	} else {
		err = errors.New("session already exists")
	}
	finishSearch(sess, err)
	return true
}
