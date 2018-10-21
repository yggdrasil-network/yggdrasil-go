package yggdrasil

// TODO signal to predecessor when we replace them?
//  Sending a ping with an extra 0 at the end of our coords should be enough to reset our throttle in their table
//  That should encorage them to ping us again sooner, and then we can reply with new info
//  Maybe remember old predecessor and check this during maintenance?

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
// It sends a ping to the old successor and predecessor, in case they're still around
func (t *dht) reset() {
	var successor *dhtInfo
	var predecessor *dhtInfo
	for infoID, info := range t.table {
		// Get current successor and predecessor
		if successor == nil || dht_ordered(&t.nodeID, &infoID, successor.getNodeID()) {
			successor = info
		}
		if predecessor == nil || dht_ordered(predecessor.getNodeID(), &infoID, &t.nodeID) {
			predecessor = info
		}
	}
	t.reqs = make(map[boxPubKey]map[NodeID]time.Time)
	t.table = make(map[NodeID]*dhtInfo)
	if successor != nil {
		t.ping(successor, nil)
	}
	if predecessor != nil {
		t.ping(predecessor, nil)
	}
}

// Does a DHT lookup and returns up to dht_lookup_size results
// If allowWorse = true, begins with best know predecessor for ID and works backwards, even if these nodes are worse predecessors than we are, to be used when intializing searches
// If allowWorse = false, begins with the best known successor for ID and works backwards (next is predecessor, etc, inclusive of the ID if it's a known node)
func (t *dht) lookup(nodeID *NodeID, allowWorse bool) []*dhtInfo {
	var results []*dhtInfo
	var successor *dhtInfo
	sTarget := t.nodeID.next()
	for infoID, info := range t.table {
		if allowWorse || dht_ordered(&t.nodeID, &infoID, nodeID) {
			results = append(results, info)
		} else {
			if successor == nil || dht_ordered(&sTarget, &infoID, successor.getNodeID()) {
				successor = info
			}
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return dht_ordered(results[j].getNodeID(), results[i].getNodeID(), nodeID)
	})
	if successor != nil {
		results = append([]*dhtInfo{successor}, results...)
	}
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
	// For bootstrapping to work, we need to add these nodes to the table
	//t.insert(&info)
	// FIXME? DEBUG testing what happens if we only add better predecessors/successors
	var successor *dhtInfo
	var predecessor *dhtInfo
	for infoID, v := range t.table {
		// Get current successor and predecessor
		if successor == nil || dht_ordered(&t.nodeID, &infoID, successor.getNodeID()) {
			successor = v
		}
		if predecessor == nil || dht_ordered(predecessor.getNodeID(), &infoID, &t.nodeID) {
			predecessor = v
		}
	}
	if successor != nil && dht_ordered(&t.nodeID, info.getNodeID(), successor.getNodeID()) {
		t.insert(&info)
	} else if predecessor != nil && dht_ordered(predecessor.getNodeID(), info.getNodeID(), &t.nodeID) {
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

// Returns nodeID + 1
func (nodeID NodeID) next() NodeID {
	for idx := len(nodeID) - 1; idx >= 0; idx-- {
		nodeID[idx] += 1
		if nodeID[idx] != 0 {
			break
		}
	}
	return nodeID
}

// Returns nodeID - 1
func (nodeID NodeID) prev() NodeID {
	for idx := len(nodeID) - 1; idx >= 0; idx-- {
		nodeID[idx] -= 1
		if nodeID[idx] != 0xff {
			break
		}
	}
	return nodeID
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
	t.insert(&rinfo) // Or at the end, after checking successor/predecessor?
	var successor *dhtInfo
	var predecessor *dhtInfo
	for infoID, info := range t.table {
		// Get current successor and predecessor
		if successor != nil && dht_ordered(&t.nodeID, &infoID, successor.getNodeID()) {
			successor = info
		}
		if predecessor != nil && dht_ordered(predecessor.getNodeID(), &infoID, &t.nodeID) {
			predecessor = info
		}
	}
	if len(res.Infos) > dht_lookup_size {
		res.Infos = res.Infos[:dht_lookup_size]
	}
	for _, info := range res.Infos {
		if *info.getNodeID() == t.nodeID {
			continue
		} // Skip self
		if _, isIn := t.table[*info.getNodeID()]; isIn {
			// TODO? don't skip if coords are different?
			continue
		}
		// Send a request to all better successors or predecessors
		// We could try sending to only the best, but then packet loss matters more
		if successor == nil || dht_ordered(&t.nodeID, info.getNodeID(), successor.getNodeID()) {
			t.ping(info, nil)
		} else if predecessor == nil || dht_ordered(predecessor.getNodeID(), info.getNodeID(), &t.nodeID) {
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
	// Ping successor, asking for their predecessor, and clean up old/expired info
	var successor *dhtInfo
	var predecessor *dhtInfo
	toPing := make(map[NodeID]*dhtInfo)
	now := time.Now()
	for infoID, info := range t.table {
		if now.Sub(info.recv) > time.Minute || info.pings > 3 {
			delete(t.table, infoID)
		} else if successor == nil || dht_ordered(&t.nodeID, &infoID, successor.getNodeID()) {
			successor = info
		} else if predecessor == nil || dht_ordered(predecessor.getNodeID(), &infoID, &t.nodeID) {
			predecessor = info
		}
	}
	//////////////////////////////////////////////////////////////////////////////
	t.core.switchTable.mutex.RLock()
	parentPort := t.core.switchTable.parent
	parentInfo := t.core.switchTable.data.peers[parentPort]
	t.core.switchTable.mutex.RUnlock()
	ports := t.core.peers.getPorts()
	if parent, isIn := ports[parentPort]; isIn {
		loc := parentInfo.locator.clone()
		end := len(loc.coords)
		if end > 0 {
			end -= 1
		}
		loc.coords = loc.coords[:end]
		pinfo := dhtInfo{key: parent.box, coords: loc.getCoords()}
		t.insert(&pinfo)
	}
	//////////////////////////////////////////////////////////////////////////////
	if successor != nil {
		toPing[*successor.getNodeID()] = successor
	}
	if predecessor != nil {
		toPing[*predecessor.getNodeID()] = predecessor
	}
	for _, info := range toPing {
		if now.Sub(info.recv) > info.throttle {
			t.ping(info, nil)
			info.pings++
			info.throttle += time.Second
			if info.throttle > 30*time.Second {
				info.throttle = 30 * time.Second
			}
			//fmt.Println("DEBUG self:", t.nodeID[:8], "throttle:", info.throttle, "nodeID:", info.getNodeID()[:8], "coords:", info.coords)
		}
	}
	return
	//////////////////////////////////////////////////////////////////////////////
	if successor != nil &&
		now.Sub(successor.recv) > successor.throttle {
		t.ping(successor, nil)
		successor.pings++
		successor.throttle += time.Second
		//return
		fmt.Println("DEBUG self:", t.nodeID[:8], "throttle:", successor.throttle, "nodeID:", successor.getNodeID()[:8], "coords:", successor.coords)
		//for infoID := range t.table {
		//	fmt.Println("DEBUG other info:", infoID[:8], "ordered", dht_ordered(&t.nodeID, &infoID, successor.getNodeID()), "swapped:", dht_ordered(&t.nodeID, successor.getNodeID(), &infoID))
		//}
		if successor.throttle > 30*time.Second {
			successor.throttle = 30 * time.Second
		}
		//fmt.Println("Table size:", len(t.table))
	}
}
