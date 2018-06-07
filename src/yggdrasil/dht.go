package yggdrasil

/*

This part has the (kademlia-like) distributed hash table

It's used to look up coords for a NodeID

Every node participates in the DHT, and the DHT stores no real keys/values
(Only the peer relationships / lookups are needed)

This version is intentionally fragile, by being recursive instead of iterative
(it's also not parallel, as a result)
This is to make sure that DHT black holes are visible if they exist
(the iterative parallel approach tends to get around them sometimes)
I haven't seen this get stuck on blackholes, but I also haven't proven it can't
Slight changes *do* make it blackhole hard, bootstrapping isn't an easy problem

*/

import "sort"
import "time"

//import "fmt"

// Maximum size for buckets and lookups
//  Exception for buckets if the next one is non-full
const dht_bucket_number = 8 * NodeIDLen // This shouldn't be changed
const dht_bucket_size = 2               // This should be at least 2
const dht_lookup_size = 16              // This should be at least 1, below 2 is impractical

type dhtInfo struct {
	nodeID_hidden *NodeID
	key           boxPubKey
	coords        []byte
	send          time.Time // When we last sent a message
	recv          time.Time // When we last received a message
	pings         int       // Decide when to drop
}

func (info *dhtInfo) getNodeID() *NodeID {
	if info.nodeID_hidden == nil {
		info.nodeID_hidden = getNodeID(&info.key)
	}
	return info.nodeID_hidden
}

type bucket struct {
	peers []*dhtInfo
	other []*dhtInfo
}

type dhtReq struct {
	Key    boxPubKey // Key of whoever asked
	Coords []byte    // Coords of whoever asked
	Dest   NodeID    // NodeID they're asking about
}

type dhtRes struct {
	Key    boxPubKey // key to respond to
	Coords []byte    // coords to respond to
	Dest   NodeID
	Infos  []*dhtInfo // response
}

type dht_rumor struct {
	info   *dhtInfo
	target *NodeID
}

type dht struct {
	core           *Core
	nodeID         NodeID
	buckets_hidden [dht_bucket_number]bucket // Extra is for the self-bucket
	peers          chan *dhtInfo             // other goroutines put incoming dht updates here
	reqs           map[boxPubKey]map[NodeID]time.Time
	offset         int
	rumorMill      []dht_rumor
}

func (t *dht) init(c *Core) {
	t.core = c
	t.nodeID = *t.core.GetNodeID()
	t.peers = make(chan *dhtInfo, 1024)
	t.reqs = make(map[boxPubKey]map[NodeID]time.Time)
}

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
	// Also (possibly) add them to our DHT
	info := dhtInfo{
		key:    req.Key,
		coords: req.Coords,
	}
	t.insertIfNew(&info, false) // This seems DoSable (we just trust their coords...)
	//if req.dest != t.nodeID { t.ping(&info, info.getNodeID()) } // Or spam...
}

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
	rinfo := dhtInfo{
		key:    res.Key,
		coords: res.Coords,
		send:   time.Now(), // Technically wrong but should be OK...
		recv:   time.Now(),
	}
	// If they're already in the table, then keep the correct send time
	bidx, isOK := t.getBucketIndex(rinfo.getNodeID())
	if !isOK {
		return
	}
	b := t.getBucket(bidx)
	for _, oldinfo := range b.peers {
		if oldinfo.key == rinfo.key {
			rinfo.send = oldinfo.send
		}
	}
	for _, oldinfo := range b.other {
		if oldinfo.key == rinfo.key {
			rinfo.send = oldinfo.send
		}
	}
	// Insert into table
	t.insert(&rinfo, false)
	if res.Dest == *rinfo.getNodeID() {
		return
	} // No infinite recursions
	if len(res.Infos) > dht_lookup_size {
		// Ignore any "extra" lookup results
		res.Infos = res.Infos[:dht_lookup_size]
	}
	for _, info := range res.Infos {
		if dht_firstCloserThanThird(info.getNodeID(), &res.Dest, rinfo.getNodeID()) {
			t.addToMill(info, info.getNodeID())
		}
	}
}

func (t *dht) lookup(nodeID *NodeID, allowCloser bool) []*dhtInfo {
	// FIXME this allocates a bunch, sorts, and keeps the part it likes
	// It would be better to only track the part it likes to begin with
	addInfos := func(res []*dhtInfo, infos []*dhtInfo) []*dhtInfo {
		for _, info := range infos {
			if info == nil {
				panic("Should never happen!")
			}
			if allowCloser || dht_firstCloserThanThird(info.getNodeID(), nodeID, &t.nodeID) {
				res = append(res, info)
			}
		}
		return res
	}
	var res []*dhtInfo
	for bidx := 0; bidx < t.nBuckets(); bidx++ {
		b := t.getBucket(bidx)
		res = addInfos(res, b.peers)
		res = addInfos(res, b.other)
	}
	doSort := func(infos []*dhtInfo) {
		less := func(i, j int) bool {
			return dht_firstCloserThanThird(infos[i].getNodeID(),
				nodeID,
				infos[j].getNodeID())
		}
		sort.SliceStable(infos, less)
	}
	doSort(res)
	if len(res) > dht_lookup_size {
		res = res[:dht_lookup_size]
	}
	return res
}

func (t *dht) getBucket(bidx int) *bucket {
	return &t.buckets_hidden[bidx]
}

func (t *dht) nBuckets() int {
	return len(t.buckets_hidden)
}

func (t *dht) insertIfNew(info *dhtInfo, isPeer bool) {
	//fmt.Println("DEBUG: dht insertIfNew:", info.getNodeID(), info.coords)
	// Insert if no "other" entry already exists
	nodeID := info.getNodeID()
	bidx, isOK := t.getBucketIndex(nodeID)
	if !isOK {
		return
	}
	b := t.getBucket(bidx)
	if (isPeer && !b.containsOther(info)) || t.shouldInsert(info) {
		// We've never heard this node before
		// TODO is there a better time than "now" to set send/recv to?
		// (Is there another "natural" choice that bootstraps faster?)
		info.send = time.Now()
		info.recv = info.send
		t.insert(info, isPeer)
	}
}

func (t *dht) insert(info *dhtInfo, isPeer bool) {
	//fmt.Println("DEBUG: dht insert:", info.getNodeID(), info.coords)
	// First update the time on this info
	info.recv = time.Now()
	// Get the bucket for this node
	nodeID := info.getNodeID()
	bidx, isOK := t.getBucketIndex(nodeID)
	if !isOK {
		return
	}
	b := t.getBucket(bidx)
	if !isPeer && !b.containsOther(info) {
		// This is a new entry, give it an old age so it's pinged sooner
		// This speeds up bootstrapping
		info.recv = info.recv.Add(-time.Hour)
	}
	// First drop any existing entry from the bucket
	b.drop(&info.key)
	// Now add to the *end* of the bucket
	if isPeer {
		// TODO make sure we don't duplicate peers in b.other too
		b.peers = append(b.peers, info)
		return
	}
	b.other = append(b.other, info)
	// Shrink from the *front* to requied size
	for len(b.other) > dht_bucket_size {
		b.other = b.other[1:]
	}
}

func (t *dht) getBucketIndex(nodeID *NodeID) (int, bool) {
	for bidx := 0; bidx < t.nBuckets(); bidx++ {
		them := nodeID[bidx/8] & (0x80 >> byte(bidx%8))
		me := t.nodeID[bidx/8] & (0x80 >> byte(bidx%8))
		if them != me {
			return bidx, true
		}
	}
	return t.nBuckets(), false
}

func dht_bucket_check(newInfo *dhtInfo, infos []*dhtInfo) bool {
	// Compares if key and coords match
	if newInfo == nil {
		panic("Should never happen")
	}
	for _, info := range infos {
		if info == nil {
			panic("Should never happen")
		}
		if info.key != newInfo.key {
			continue
		}
		if len(info.coords) != len(newInfo.coords) {
			continue
		}
		match := true
		for idx := 0; idx < len(info.coords); idx++ {
			if info.coords[idx] != newInfo.coords[idx] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func (b *bucket) containsPeer(info *dhtInfo) bool {
	return dht_bucket_check(info, b.peers)
}

func (b *bucket) containsOther(info *dhtInfo) bool {
	return dht_bucket_check(info, b.other)
}

func (b *bucket) contains(info *dhtInfo) bool {
	return b.containsPeer(info) || b.containsOther(info)
}

func (b *bucket) drop(key *boxPubKey) {
	clean := func(infos []*dhtInfo) []*dhtInfo {
		cleaned := infos[:0]
		for _, info := range infos {
			if info.key == *key {
				continue
			}
			cleaned = append(cleaned, info)
		}
		return cleaned
	}
	b.peers = clean(b.peers)
	b.other = clean(b.other)
}

func (t *dht) sendReq(req *dhtReq, dest *dhtInfo) {
	// Send a dhtReq to the node in dhtInfo
	bs := req.encode()
	shared := t.core.sessions.getSharedKey(&t.core.boxPriv, &dest.key)
	payload, nonce := boxSeal(shared, bs, nil)
	p := wire_protoTrafficPacket{
		TTL:     ^uint64(0),
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

func (t *dht) sendRes(res *dhtRes, req *dhtReq) {
	// Send a reply for a dhtReq
	bs := res.encode()
	shared := t.core.sessions.getSharedKey(&t.core.boxPriv, &req.Key)
	payload, nonce := boxSeal(shared, bs, nil)
	p := wire_protoTrafficPacket{
		TTL:     ^uint64(0),
		Coords:  req.Coords,
		ToKey:   req.Key,
		FromKey: t.core.boxPub,
		Nonce:   *nonce,
		Payload: payload,
	}
	packet := p.encode()
	t.core.router.out(packet)
}

func (b *bucket) isEmpty() bool {
	return len(b.peers)+len(b.other) == 0
}

func (b *bucket) nextToPing() *dhtInfo {
	// Check the nodes in the bucket
	// Return whichever one responded least recently
	// Delay of 6 seconds between pinging the same node
	//  Gives them time to respond
	//  And time between traffic loss from short term congestion in the network
	var toPing *dhtInfo
	update := func(infos []*dhtInfo) {
		for _, next := range infos {
			if time.Since(next.send) < 6*time.Second {
				continue
			}
			if toPing == nil || next.recv.Before(toPing.recv) {
				toPing = next
			}
		}
	}
	update(b.peers)
	update(b.other)
	return toPing
}

func (t *dht) getTarget(bidx int) *NodeID {
	targetID := t.nodeID
	targetID[bidx/8] ^= 0x80 >> byte(bidx%8)
	return &targetID
}

func (t *dht) ping(info *dhtInfo, target *NodeID) {
	if info.pings > 2 {
		bidx, isOK := t.getBucketIndex(info.getNodeID())
		if !isOK {
			panic("This should never happen")
		}
		b := t.getBucket(bidx)
		b.drop(&info.key)
		return
	}
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
	info.pings++
	info.send = time.Now()
	t.sendReq(&req, info)
}

func (t *dht) addToMill(info *dhtInfo, target *NodeID) {
	rumor := dht_rumor{
		info:   info,
		target: target,
	}
	t.rumorMill = append(t.rumorMill, rumor)
}

func (t *dht) doMaintenance() {
	// First clean up reqs
	for key, reqs := range t.reqs {
		for target, timeout := range reqs {
			if time.Since(timeout) > time.Minute {
				delete(reqs, target)
			}
		}
		if len(reqs) == 0 {
			delete(t.reqs, key)
		}
	}
	if len(t.rumorMill) == 0 {
		// Ping the least recently contacted node
		//  This is to make sure we eventually notice when someone times out
		var oldest *dhtInfo
		last := 0
		for bidx := 0; bidx < t.nBuckets(); bidx++ {
			b := t.getBucket(bidx)
			if !b.isEmpty() {
				last = bidx
				toPing := b.nextToPing()
				if toPing == nil {
					continue
				} // We've recently pinged everyone in b
				if oldest == nil || toPing.recv.Before(oldest.recv) {
					oldest = toPing
				}
			}
		}
		if oldest != nil && time.Since(oldest.recv) > time.Minute {
			t.addToMill(oldest, nil)
		} // if the DHT isn't empty
		// Refresh buckets
		if t.offset > last {
			t.offset = 0
		}
		target := t.getTarget(t.offset)
		for _, info := range t.lookup(target, true) {
			if time.Since(info.recv) > time.Minute {
				t.addToMill(info, target)
				t.offset++
				break
			}
		}
		//t.offset++
	}
	for len(t.rumorMill) > 0 {
		var rumor dht_rumor
		rumor, t.rumorMill = t.rumorMill[0], t.rumorMill[1:]
		if rumor.target == rumor.info.getNodeID() {
			// Note that the above is a pointer comparison, and target can be nil
			// This is only for adding new nodes (learned from other lookups)
			// It only makes sense to ping if the node isn't already in the table
			if !t.shouldInsert(rumor.info) {
				continue
			}
		}
		t.ping(rumor.info, rumor.target)
		break
	}
}

func (t *dht) shouldInsert(info *dhtInfo) bool {
	bidx, isOK := t.getBucketIndex(info.getNodeID())
	if !isOK {
		return false
	}
	b := t.getBucket(bidx)
	if b.containsOther(info) {
		return false
	}
	if len(b.other) < dht_bucket_size {
		return true
	}
	for _, other := range b.other {
		if dht_firstCloserThanThird(info.getNodeID(), &t.nodeID, other.getNodeID()) {
			return true
		}
	}
	return false
}

func dht_firstCloserThanThird(first *NodeID,
	second *NodeID,
	third *NodeID) bool {
	for idx := 0; idx < NodeIDLen; idx++ {
		f := first[idx] ^ second[idx]
		t := third[idx] ^ second[idx]
		if f == t {
			continue
		}
		return f < t
	}
	return false
}

func (t *dht) reset() {
	// This is mostly so bootstrapping will reset to resend coords into the network
	t.offset = 0
	t.rumorMill = nil // reset mill
	for _, b := range t.buckets_hidden {
		b.peers = b.peers[:0]
		for _, info := range b.other {
			// Add other nodes to the rumor mill so they'll be pinged soon
			// This will hopefully tell them our coords and re-learn theirs quickly if they haven't changed
			t.addToMill(info, info.getNodeID())
		}
		b.other = b.other[:0]
	}
}
