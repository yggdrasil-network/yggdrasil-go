package yggdrasil

// TODO signal to predecessor when we replace them?
//  Sending a ping with an extra 0 at the end of our coords should be enough to reset our throttle in their table
//  That should encorage them to ping us again sooner, and then we can reply with new info
//  Maybe remember old predecessor and check this during maintenance?

// TODO make sure that, if your peer is your successor or predecessor, you still bother to ping them and ask for better nodes
//  Basically, don't automatically reset the dhtInfo.recv to time.Now() whenever updating them from the outside
//  But *do* set it to something that won't instantly time them out or make them get pingspammed?
//  Could set throttle to 0, but that's imperfect at best... pingspam

// TODO? cache all nodes we ping (from e.g. searches), not just the important ones
//  But only send maintenance pings to the important ones

// TODO reoptimize search stuff (size, timeouts, etc) to play nicer with DHT churn

import (
	"fmt"
	"sort"
	"time"
)

const dht_lookup_size = 16

// dhtInfo represents everything we know about a node in the DHT.
// This includes its key, a cache of it's NodeID, coords, and timing/ping related info for deciding who/when to ping nodes for maintenance.
type dhtInfo struct {
	nodeID_hidden *NodeID
	key           boxPubKey
	coords        []byte
	recv          time.Time // When we last received a message
	pings         int       // Time out if at least 3 consecutive maintenance pings drop
	throttle      time.Duration
}

// Returns the *NodeID associated with dhtInfo.key, calculating it on the fly the first time or from a cache all subsequent times.
func (info *dhtInfo) getNodeID() *NodeID {
	if info.nodeID_hidden == nil {
		info.nodeID_hidden = getNodeID(&info.key)
	}
	return info.nodeID_hidden
}

// Request for a node to do a lookup.
// Includes our key and coords so they can send a response back, and the destination NodeID we want to ask about.
type dhtReq struct {
	Key    boxPubKey // Key of whoever asked
	Coords []byte    // Coords of whoever asked
	Dest   NodeID    // NodeID they're asking about
}

// Response to a DHT lookup.
// Includes the key and coords of the node that's responding, and the destination they were asked about.
// The main part is Infos []*dhtInfo, the lookup response.
type dhtRes struct {
	Key    boxPubKey // key of the sender
	Coords []byte    // coords of the sender
	Dest   NodeID
	Infos  []*dhtInfo // response
}

// The main DHT struct.
type dht struct {
	core   *Core
	nodeID NodeID
	table  map[NodeID]*dhtInfo
	peers  chan *dhtInfo // other goroutines put incoming dht updates here
	reqs   map[boxPubKey]map[NodeID]time.Time
}

// Initializes the DHT
func (t *dht) init(c *Core) {
	t.core = c
	t.nodeID = *t.core.GetNodeID()
	t.peers = make(chan *dhtInfo, 1024)
	t.reset()
}

// Resets the DHT in response to coord changes
// This empties all info from the DHT and drops outstanding requests
func (t *dht) reset() {
	t.reqs = make(map[boxPubKey]map[NodeID]time.Time)
	t.table = make(map[NodeID]*dhtInfo)
}

// Does a DHT lookup and returns up to dht_lookup_size results
func (t *dht) lookup(nodeID *NodeID, everything bool) []*dhtInfo {
	results := make([]*dhtInfo, 0, len(t.table))
	//imp := t.getImportant()
	for _, info := range t.table {
		results = append(results, info)
		//if t.isImportant(info, imp) {
		//	results = append(results, info)
		//}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return dht_ordered(nodeID, results[i].getNodeID(), results[j].getNodeID())
	})
	if len(results) > dht_lookup_size {
		results = results[:dht_lookup_size]
	}
	return results
}

// Insert into table, preserving the time we last sent a packet if the node was already in the table, otherwise setting that time to now
func (t *dht) insert(info *dhtInfo) {
	if *info.getNodeID() == t.nodeID {
		// This shouldn't happen, but don't add it if it does
		return
		panic("FIXME")
	}
	info.recv = time.Now()
	if oldInfo, isIn := t.table[*info.getNodeID()]; isIn {
		sameCoords := true
		if len(info.coords) != len(oldInfo.coords) {
			sameCoords = false
		} else {
			for idx := 0; idx < len(info.coords); idx++ {
				if info.coords[idx] != oldInfo.coords[idx] {
					sameCoords = false
					break
				}
			}
		}
		if sameCoords {
			info.throttle = oldInfo.throttle
		}
	}
	t.table[*info.getNodeID()] = info
}

// Return true if first/second/third are (partially) ordered correctly
//  FIXME? maybe total ordering makes more sense
func dht_ordered(first, second, third *NodeID) bool {
	lessOrEqual := func(first, second *NodeID) bool {
		for idx := 0; idx < NodeIDLen; idx++ {
			if first[idx] > second[idx] {
				return false
			}
			if first[idx] < second[idx] {
				return true
			}
		}
		return true
	}
	firstLessThanSecond := lessOrEqual(first, second)
	secondLessThanThird := lessOrEqual(second, third)
	thirdLessThanFirst := lessOrEqual(third, first)
	switch {
	case firstLessThanSecond && secondLessThanThird:
		// Nothing wrapped around 0, the easy case
		return true
	case thirdLessThanFirst && firstLessThanSecond:
		// Third wrapped around 0
		return true
	case secondLessThanThird && thirdLessThanFirst:
		// Second (and third) wrapped around 0
		return true
	}
	return false
}

// Reads a request, performs a lookup, and responds.
// Update info about the node that sent the request.
func (t *dht) handleReq(req *dhtReq) {
	// Send them what they asked for
	loc := t.core.switchTable.getLocator()
	coords := loc.getCoords()
	res := dhtRes{
		Key:    t.core.boxPub,
		Coords: coords,
		Dest:   req.Dest,
		Infos:  t.lookup(&req.Dest, false),
	}
	t.sendRes(&res, req)
	// Also add them to our DHT
	info := dhtInfo{
		key:    req.Key,
		coords: req.Coords,
	}
	imp := t.getImportant()
	if _, isIn := t.table[*info.getNodeID()]; !isIn || t.isImportant(&info, imp) {
		t.insert(&info)
	}
}

// Sends a lookup response to the specified node.
func (t *dht) sendRes(res *dhtRes, req *dhtReq) {
	// Send a reply for a dhtReq
	bs := res.encode()
	shared := t.core.sessions.getSharedKey(&t.core.boxPriv, &req.Key)
	payload, nonce := boxSeal(shared, bs, nil)
	p := wire_protoTrafficPacket{
		Coords:  req.Coords,
		ToKey:   req.Key,
		FromKey: t.core.boxPub,
		Nonce:   *nonce,
		Payload: payload,
	}
	packet := p.encode()
	t.core.router.out(packet)
}

// Reads a lookup response, checks that we had sent a matching request, and processes the response info.
// This mainly consists of updating the node we asked in our DHT (they responded, so we know they're still alive), and deciding if we want to do anything with their responses
func (t *dht) handleRes(res *dhtRes) {
	t.core.searches.handleDHTRes(res)
	reqs, isIn := t.reqs[res.Key]
	if !isIn {
		return
	}
	_, isIn = reqs[res.Dest]
	if !isIn {
		return
	}
	delete(reqs, res.Dest)
	rinfo := dhtInfo{
		key:    res.Key,
		coords: res.Coords,
	}
	imp := t.getImportant()
	if t.isImportant(&rinfo, imp) {
		t.insert(&rinfo)
	}
	//t.insert(&rinfo) // Or at the end, after checking successor/predecessor?
	if len(res.Infos) > dht_lookup_size {
		//res.Infos = res.Infos[:dht_lookup_size] //FIXME debug
	}
	for _, info := range res.Infos {
		if *info.getNodeID() == t.nodeID {
			continue
		} // Skip self
		if _, isIn := t.table[*info.getNodeID()]; isIn {
			// TODO? don't skip if coords are different?
			continue
		}
		if t.isImportant(info, imp) {
			t.ping(info, nil)
		}
	}
	// TODO add everyting else to a rumor mill for later use? (when/how?)
}

// Sends a lookup request to the specified node.
func (t *dht) sendReq(req *dhtReq, dest *dhtInfo) {
	// Send a dhtReq to the node in dhtInfo
	bs := req.encode()
	shared := t.core.sessions.getSharedKey(&t.core.boxPriv, &dest.key)
	payload, nonce := boxSeal(shared, bs, nil)
	p := wire_protoTrafficPacket{
		Coords:  dest.coords,
		ToKey:   dest.key,
		FromKey: t.core.boxPub,
		Nonce:   *nonce,
		Payload: payload,
	}
	packet := p.encode()
	t.core.router.out(packet)
	reqsToDest, isIn := t.reqs[dest.key]
	if !isIn {
		t.reqs[dest.key] = make(map[NodeID]time.Time)
		reqsToDest, isIn = t.reqs[dest.key]
		if !isIn {
			panic("This should never happen")
		}
	}
	reqsToDest[req.Dest] = time.Now()
}

func (t *dht) ping(info *dhtInfo, target *NodeID) {
	// Creates a req for the node at dhtInfo, asking them about the target (if one is given) or themself (if no target is given)
	if target == nil {
		target = &t.nodeID
	}
	loc := t.core.switchTable.getLocator()
	coords := loc.getCoords()
	req := dhtReq{
		Key:    t.core.boxPub,
		Coords: coords,
		Dest:   *target,
	}
	t.sendReq(&req, info)
}

func (t *dht) doMaintenance() {
	toPing := make(map[NodeID]*dhtInfo)
	now := time.Now()
	imp := t.getImportant()
	good := make(map[NodeID]*dhtInfo)
	for _, info := range imp {
		good[*info.getNodeID()] = info
	}
	for infoID, info := range t.table {
		if now.Sub(info.recv) > time.Minute || info.pings > 3 {
			delete(t.table, infoID)
		} else if t.isImportant(info, imp) {
			toPing[infoID] = info
		}
	}
	for _, info := range toPing {
		if now.Sub(info.recv) > info.throttle {
			t.ping(info, info.getNodeID())
			info.pings++
			info.throttle += time.Second
			if info.throttle > 30*time.Second {
				info.throttle = 30 * time.Second
			}
			continue
			fmt.Println("DEBUG self:", t.nodeID[:8], "throttle:", info.throttle, "nodeID:", info.getNodeID()[:8], "coords:", info.coords)
		}
	}
	return // Skip printing debug info
	var out []interface{}
	out = append(out, "DEBUG important:")
	out = append(out, t.nodeID[:8])
	for _, info := range imp {
		out = append(out, info.getNodeID()[:8])
	}
	fmt.Println(out...)
}

func (t *dht) getImportant() []*dhtInfo {
	// Get a list of all known nodes
	infos := make([]*dhtInfo, 0, len(t.table))
	for _, info := range t.table {
		infos = append(infos, info)
	}
	// Sort them by increasing order in distance along the ring
	sort.SliceStable(infos, func(i, j int) bool {
		// Sort in order of predecessors (!), reverse from chord normal, because it plays nicer with zero bits for unknown parts of target addresses
		return dht_ordered(infos[j].getNodeID(), infos[i].getNodeID(), &t.nodeID)
	})
	// Keep the ones that are no further than the closest seen so far
	minDist := ^uint64(0)
	loc := t.core.switchTable.getLocator()
	important := infos[:0]
	for _, info := range infos {
		dist := uint64(loc.dist(info.coords))
		if dist < minDist {
			minDist = dist
			important = append(important, info)
		}
	}
	var temp []*dhtInfo
	minDist = ^uint64(0)
	for idx := len(infos) - 1; idx >= 0; idx-- {
		info := infos[idx]
		dist := uint64(loc.dist(info.coords))
		if dist < minDist {
			minDist = dist
			temp = append(temp, info)
		}
	}
	for idx := len(temp) - 1; idx >= 0; idx-- {
		important = append(important, temp[idx])
	}
	return important
}

func (t *dht) isImportant(ninfo *dhtInfo, important []*dhtInfo) bool {
	// Check if ninfo is of equal or greater importance to what we already know
	loc := t.core.switchTable.getLocator()
	ndist := uint64(loc.dist(ninfo.coords))
	minDist := ^uint64(0)
	for _, info := range important {
		if (*info.getNodeID() == *ninfo.getNodeID()) ||
			(ndist < minDist && dht_ordered(info.getNodeID(), ninfo.getNodeID(), &t.nodeID)) {
			// Either the same node, or a better one
			return true
		}
		dist := uint64(loc.dist(info.coords))
		if dist < minDist {
			minDist = dist
		}
	}
	minDist = ^uint64(0)
	for idx := len(important) - 1; idx >= 0; idx-- {
		info := important[idx]
		if (*info.getNodeID() == *ninfo.getNodeID()) ||
			(ndist < minDist && dht_ordered(&t.nodeID, ninfo.getNodeID(), info.getNodeID())) {
			// Either the same node, or a better one
			return true
		}
		dist := uint64(loc.dist(info.coords))
		if dist < minDist {
			minDist = dist
		}
	}
	// We didn't find any important node that ninfo is better than
	return false
}
