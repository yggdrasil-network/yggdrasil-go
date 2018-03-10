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
const dht_bucket_size = 2               // This should be at least 2
const dht_lookup_size = 2               // This should be at least 1, below 2 is impractical
const dht_bucket_number = 8 * NodeIDLen // This shouldn't be changed

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
	key    boxPubKey // Key of whoever asked
	coords []byte    // Coords of whoever asked
	dest   NodeID    // NodeID they're asking about
}

type dhtRes struct {
	key    boxPubKey // key to respond to
	coords []byte    // coords to respond to
	dest   NodeID
	infos  []*dhtInfo // response
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
	t.peers = make(chan *dhtInfo, 1)
	t.reqs = make(map[boxPubKey]map[NodeID]time.Time)
}

func (t *dht) handleReq(req *dhtReq) {
	// Send them what they asked for
	loc := t.core.switchTable.getLocator()
	coords := loc.getCoords()
	res := dhtRes{
		key:    t.core.boxPub,
		coords: coords,
		dest:   req.dest,
		infos:  t.lookup(&req.dest),
	}
	t.sendRes(&res, req)
	// Also (possibly) add them to our DHT
	info := dhtInfo{
		key:    req.key,
		coords: req.coords,
	}
	t.insertIfNew(&info, false) // This seems DoSable (we just trust their coords...)
	//if req.dest != t.nodeID { t.ping(&info, info.getNodeID()) } // Or spam...
}

func (t *dht) handleRes(res *dhtRes) {
	reqs, isIn := t.reqs[res.key]
	if !isIn {
		return
	}
	_, isIn = reqs[res.dest]
	if !isIn {
		return
	}
	rinfo := dhtInfo{
		key:    res.key,
		coords: res.coords,
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
	if res.dest == *rinfo.getNodeID() {
		return
	} // No infinite recursions
	// ping the nodes we were told about
	if len(res.infos) > dht_lookup_size {
		// Ignore any "extra" lookup results
		res.infos = res.infos[:dht_lookup_size]
	}
	for _, info := range res.infos {
		bidx, isOK := t.getBucketIndex(info.getNodeID())
		if !isOK {
			continue
		}
		b := t.getBucket(bidx)
		if b.contains(info) {
			continue
		} // wait for maintenance cycle to get them
		t.addToMill(info, info.getNodeID())
	}
}

func (t *dht) lookup(nodeID *NodeID) []*dhtInfo {
	// FIXME this allocates a bunch, sorts, and keeps the part it likes
	// It would be better to only track the part it likes to begin with
	addInfos := func(res []*dhtInfo, infos []*dhtInfo) []*dhtInfo {
		for _, info := range infos {
			if info == nil {
				panic("Should never happen!")
			}
			if true || dht_firstCloserThanThird(info.getNodeID(), nodeID, &t.nodeID) {
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
	// Insert a peer if and only if the bucket doesn't already contain it
	nodeID := info.getNodeID()
	bidx, isOK := t.getBucketIndex(nodeID)
	if !isOK {
		return
	}
	b := t.getBucket(bidx)
	if !b.contains(info) {
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
	// First drop any existing entry from the bucket
	b.drop(&info.key)
	// Now add to the *end* of the bucket
	if isPeer {
		// TODO make sure we don't duplicate peers in b.other too
		b.peers = append(b.peers, info)
		return
	}
	b.other = append(b.other, info)
	// Check if the next bucket is non-full and return early if it is
	if bidx+1 == t.nBuckets() {
		return
	}
	bnext := t.getBucket(bidx + 1)
	if len(bnext.other) < dht_bucket_size {
		return
	}
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

func (b *bucket) contains(ninfo *dhtInfo) bool {
	// Compares if key and coords match
	var found bool
	check := func(infos []*dhtInfo) {
		for _, info := range infos {
			if info == nil {
				panic("Should never happen")
			}
			if info.key != info.key {
				continue
			}
			if len(info.coords) != len(ninfo.coords) {
				continue
			}
			match := true
			for idx := 0; idx < len(info.coords); idx++ {
				if info.coords[idx] != ninfo.coords[idx] {
					match = false
					break
				}
			}
			if match {
				found = true
				break
			}
		}
	}
	check(b.peers)
	check(b.other)
	return found
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
		ttl:     ^uint64(0),
		coords:  dest.coords,
		toKey:   dest.key,
		fromKey: t.core.boxPub,
		nonce:   *nonce,
		payload: payload,
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
	reqsToDest[req.dest] = time.Now()
}

func (t *dht) sendRes(res *dhtRes, req *dhtReq) {
	// Send a reply for a dhtReq
	bs := res.encode()
	shared := t.core.sessions.getSharedKey(&t.core.boxPriv, &req.key)
	payload, nonce := boxSeal(shared, bs, nil)
	p := wire_protoTrafficPacket{
		ttl:     ^uint64(0),
		coords:  req.coords,
		toKey:   req.key,
		fromKey: t.core.boxPub,
		nonce:   *nonce,
		payload: payload,
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
		key:    t.core.boxPub,
		coords: coords,
		dest:   *target,
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
		if oldest != nil {
			t.addToMill(oldest, nil)
		} // if the DHT isn't empty
		// Refresh buckets
		if t.offset > last {
			t.offset = 0
		}
		target := t.getTarget(t.offset)
		for _, info := range t.lookup(target) {
			t.addToMill(info, target)
			break
		}
		t.offset++
	}
	if len(t.rumorMill) > 0 {
		var rumor dht_rumor
		rumor, t.rumorMill = t.rumorMill[0], t.rumorMill[1:]
		t.ping(rumor.info, rumor.target)
	}
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
