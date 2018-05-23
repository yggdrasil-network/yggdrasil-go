package yggdrasil

// This part constructs a spanning tree of the network
// It routes packets based on distance on the spanning tree
//  In general, this is *not* equivalent to routing on the tree
//  It falls back to the tree in the worst case, but it can take shortcuts too
// This is the part that makse routing reasonably efficient on scale-free graphs

// TODO document/comment everything in a lot more detail

// TODO? use a pre-computed lookup table (python version had this)
//  A little annoying to do with constant changes from bandwidth estimates

import "time"
import "sync"
import "sync/atomic"

//import "fmt"

const switch_timeout = time.Minute
const switch_updateInterval = switch_timeout / 2
const switch_throttle = switch_updateInterval / 2

// You should be able to provide crypto signatures for this
// 1 signature per coord, from the *sender* to that coord
// E.g. A->B->C has sigA(A->B) and sigB(A->B->C)
type switchLocator struct {
	root   sigPubKey
	tstamp int64
	coords []switchPort
}

func firstIsBetter(first, second *sigPubKey) bool {
	// Higher TreeID is better
	ftid := getTreeID(first)
	stid := getTreeID(second)
	for idx := 0; idx < len(ftid); idx++ {
		if ftid[idx] == stid[idx] {
			continue
		}
		return ftid[idx] > stid[idx]
	}
	// Edge case, when comparing identical IDs
	return false
}

func (l *switchLocator) clone() switchLocator {
	// Used to create a deep copy for use in messages
	// Copy required because we need to mutate coords before sending
	// (By appending the port from us to the destination)
	loc := *l
	loc.coords = make([]switchPort, len(l.coords), len(l.coords)+1)
	copy(loc.coords, l.coords)
	return loc
}

func (l *switchLocator) dist(dest []byte) int {
	// Returns distance (on the tree) from these coords
	offset := 0
	fdc := 0
	for {
		if fdc >= len(l.coords) {
			break
		}
		coord, length := wire_decode_uint64(dest[offset:])
		if length == 0 {
			break
		}
		if l.coords[fdc] != switchPort(coord) {
			break
		}
		fdc++
		offset += length
	}
	dist := len(l.coords[fdc:])
	for {
		_, length := wire_decode_uint64(dest[offset:])
		if length == 0 {
			break
		}
		dist++
		offset += length
	}
	return dist
}

func (l *switchLocator) getCoords() []byte {
	bs := make([]byte, 0, len(l.coords))
	for _, coord := range l.coords {
		c := wire_encode_uint64(uint64(coord))
		bs = append(bs, c...)
	}
	return bs
}

func (x *switchLocator) isAncestorOf(y *switchLocator) bool {
	if x.root != y.root {
		return false
	}
	if len(x.coords) > len(y.coords) {
		return false
	}
	for idx := range x.coords {
		if x.coords[idx] != y.coords[idx] {
			return false
		}
	}
	return true
}

type peerInfo struct {
	key       sigPubKey     // ID of this peer
	locator   switchLocator // Should be able to respond with signatures upon request
	degree    uint64        // Self-reported degree
	coords    []switchPort  // Coords of this peer (taken from coords of the sent locator)
	time      time.Time     // Time this node was last seen
	firstSeen time.Time
	port      switchPort // Interface number of this peer
	seq       uint64     // Seq number we last saw this peer advertise
}

type switchMessage struct {
	from    sigPubKey     // key of the sender
	locator switchLocator // Locator advertised for the receiver, not the sender's loc!
	seq     uint64
}

type switchPort uint64
type tableElem struct {
	port    switchPort
	locator switchLocator
}

type lookupTable struct {
	self  switchLocator
	elems []tableElem
}

type switchData struct {
	// All data that's mutable and used by exported Table methods
	// To be read/written with atomic.Value Store/Load calls
	locator switchLocator
	seq     uint64 // Sequence number, reported to peers, so they know about changes
	peers   map[switchPort]peerInfo
	sigs    []sigInfo
}

type switchTable struct {
	core    *Core
	key     sigPubKey           // Our own key
	time    time.Time           // Time when locator.tstamp was last updated
	parent  switchPort          // Port of whatever peer is our parent, or self if we're root
	drop    map[sigPubKey]int64 // Tstamp associated with a dropped root
	mutex   sync.RWMutex        // Lock for reads/writes of switchData
	data    switchData
	updater atomic.Value //*sync.Once
	table   atomic.Value //lookupTable
}

func (t *switchTable) init(core *Core, key sigPubKey) {
	now := time.Now()
	t.core = core
	t.key = key
	locator := switchLocator{root: key, tstamp: now.Unix()}
	peers := make(map[switchPort]peerInfo)
	t.data = switchData{locator: locator, peers: peers}
	t.updater.Store(&sync.Once{})
	t.table.Store(lookupTable{})
	t.drop = make(map[sigPubKey]int64)
}

func (t *switchTable) start() {
	doTicker := func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			t.Tick()
		}
	}
	go doTicker()
}

func (t *switchTable) getLocator() switchLocator {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.data.locator.clone()
}

func (t *switchTable) Tick() {
	// Periodic maintenance work to keep things internally consistent
	t.mutex.Lock()         // Write lock
	defer t.mutex.Unlock() // Release lock when we're done
	t.cleanRoot()
	t.cleanPeers()
	t.cleanDropped()
}

func (t *switchTable) cleanRoot() {
	// TODO rethink how this is done?...
	// Get rid of the root if it looks like its timed out
	now := time.Now()
	doUpdate := false
	//fmt.Println("DEBUG clean root:", now.Sub(t.time))
	if now.Sub(t.time) > switch_timeout {
		//fmt.Println("root timed out", t.data.locator)
		dropped := t.data.peers[t.parent]
		dropped.time = t.time
		t.drop[t.data.locator.root] = t.data.locator.tstamp
		doUpdate = true
		//t.core.log.Println("DEBUG: switch root timeout", len(t.drop))
	}
	// Or, if we're better than our root, root ourself
	if firstIsBetter(&t.key, &t.data.locator.root) {
		//fmt.Println("root is worse than us", t.data.locator.Root)
		doUpdate = true
		//t.core.log.Println("DEBUG: switch root replace with self", t.data.locator.Root)
	}
	// Or, if we are the root, possibly update our timestamp
	if t.data.locator.root == t.key &&
		now.Sub(t.time) > switch_updateInterval {
		//fmt.Println("root is self and old, updating", t.data.locator.Root)
		doUpdate = true
	}
	if doUpdate {
		t.parent = switchPort(0)
		t.time = now
		if t.data.locator.root != t.key {
			t.data.seq++
			t.updater.Store(&sync.Once{})
			select {
			case t.core.router.reset <- struct{}{}:
			default:
			}
		}
		t.data.locator = switchLocator{root: t.key, tstamp: now.Unix()}
		t.data.sigs = nil
	}
}

func (t *switchTable) cleanPeers() {
	now := time.Now()
	changed := false
	for idx, info := range t.data.peers {
		if info.port != switchPort(0) && now.Sub(info.time) > 6*time.Second /*switch_timeout*/ {
			//fmt.Println("peer timed out", t.key, info.locator)
			delete(t.data.peers, idx)
			changed = true
		}
	}
	if changed {
		t.updater.Store(&sync.Once{})
	}
}

func (t *switchTable) cleanDropped() {
	// TODO? only call this after root changes, not periodically
	for root := range t.drop {
		if !firstIsBetter(&root, &t.data.locator.root) {
			delete(t.drop, root)
		}
	}
}

func (t *switchTable) createMessage(port switchPort) (*switchMessage, []sigInfo) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	msg := switchMessage{from: t.key, locator: t.data.locator.clone()}
	msg.locator.coords = append(msg.locator.coords, port)
	msg.seq = t.data.seq
	return &msg, t.data.sigs
}

func (t *switchTable) handleMessage(msg *switchMessage, fromPort switchPort, sigs []sigInfo) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	now := time.Now()
	if len(msg.locator.coords) == 0 {
		return
	} // Should always have >=1 links
	oldSender, isIn := t.data.peers[fromPort]
	if !isIn {
		oldSender.firstSeen = now
	}
	sender := peerInfo{key: msg.from,
		locator:   msg.locator,
		coords:    msg.locator.coords[:len(msg.locator.coords)-1],
		time:      now,
		firstSeen: oldSender.firstSeen,
		port:      fromPort,
		seq:       msg.seq}
	equiv := func(x *switchLocator, y *switchLocator) bool {
		if x.root != y.root {
			return false
		}
		if len(x.coords) != len(y.coords) {
			return false
		}
		for idx := range x.coords {
			if x.coords[idx] != y.coords[idx] {
				return false
			}
		}
		return true
	}
	doUpdate := false
	if !equiv(&msg.locator, &oldSender.locator) {
		doUpdate = true
	}
	t.data.peers[fromPort] = sender
	updateRoot := false
	oldParent, isIn := t.data.peers[t.parent]
	noParent := !isIn
	noLoop := func() bool {
		for idx := 0; idx < len(sigs)-1; idx++ {
			if sigs[idx].next == t.core.sigPub {
				return false
			}
		}
		if msg.locator.root == t.core.sigPub {
			return false
		}
		return true
	}()
	sTime := now.Sub(sender.firstSeen)
	pTime := oldParent.time.Sub(oldParent.firstSeen) + switch_timeout
	// Really want to compare sLen/sTime and pLen/pTime
	// Cross multiplied to avoid divide-by-zero
	cost := len(msg.locator.coords) * int(pTime.Seconds())
	pCost := len(t.data.locator.coords) * int(sTime.Seconds())
	dropTstamp, isIn := t.drop[msg.locator.root]
	// Here be dragons
	switch {
	case !noLoop: // do nothing
	case isIn && dropTstamp >= msg.locator.tstamp: // do nothing
	case firstIsBetter(&msg.locator.root, &t.data.locator.root):
		updateRoot = true
	case t.data.locator.root != msg.locator.root: // do nothing
	case t.data.locator.tstamp > msg.locator.tstamp: // do nothing
	case noParent:
		updateRoot = true
	case cost < pCost:
		updateRoot = true
	case sender.port != t.parent: // do nothing
	case !equiv(&msg.locator, &t.data.locator):
		updateRoot = true
	case now.Sub(t.time) < switch_throttle: // do nothing
	case msg.locator.tstamp > t.data.locator.tstamp:
		updateRoot = true
	}
	if updateRoot {
		if !equiv(&msg.locator, &t.data.locator) {
			doUpdate = true
			t.data.seq++
			select {
			case t.core.router.reset <- struct{}{}:
			default:
			}
			//t.core.log.Println("Switch update:", msg.locator.root, msg.locator.tstamp, msg.locator.coords)
			//fmt.Println("Switch update:", msg.Locator.Root, msg.Locator.Tstamp, msg.Locator.Coords)
		}
		if t.data.locator.tstamp != msg.locator.tstamp {
			t.time = now
		}
		t.data.locator = msg.locator
		t.parent = sender.port
		t.data.sigs = sigs
		//t.core.log.Println("Switch update:", msg.Locator.Root, msg.Locator.Tstamp, msg.Locator.Coords)
	}
	if doUpdate {
		t.updater.Store(&sync.Once{})
	}
	return
}

func (t *switchTable) updateTable() {
	// WARNING this should only be called from within t.data.updater.Do()
	//  It relies on the sync.Once for synchronization with messages and lookups
	// TODO use a pre-computed faster lookup table
	//  Instead of checking distance for every destination every time
	//  Array of structs, indexed by first coord that differs from self
	//  Each struct has stores the best port to forward to, and a next coord map
	//  Move to struct, then iterate over coord maps until you dead end
	//  The last port before the dead end should be the closest
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	newTable := lookupTable{
		self:  t.data.locator.clone(),
		elems: make([]tableElem, 0, len(t.data.peers)),
	}
	for _, pinfo := range t.data.peers {
		//if !pinfo.forward { continue }
		if pinfo.locator.root != newTable.self.root {
			continue
		}
		loc := pinfo.locator.clone()
		loc.coords = loc.coords[:len(loc.coords)-1] // Remove the them->self link
		newTable.elems = append(newTable.elems, tableElem{
			locator: loc,
			port:    pinfo.port,
		})
	}
	t.table.Store(newTable)
}

func (t *switchTable) lookup(dest []byte, ttl uint64) (switchPort, uint64) {
	t.updater.Load().(*sync.Once).Do(t.updateTable)
	table := t.table.Load().(lookupTable)
	ports := t.core.peers.getPorts()
	getBandwidth := func(port switchPort) float64 {
		var bandwidth float64
		if p, isIn := ports[port]; isIn {
			bandwidth = p.getBandwidth()
		}
		return bandwidth
	}
	var best switchPort
	myDist := table.self.dist(dest) //getDist(table.self.coords)
	if !(uint64(myDist) < ttl) {
		return 0, 0
	}
	// score is in units of bandwidth / distance
	bestScore := float64(-1)
	for _, info := range table.elems {
		dist := info.locator.dist(dest) //getDist(info.locator.coords)
		if !(dist < myDist) {
			continue
		}
		score := getBandwidth(info.port)
		score /= float64(1 + dist)
		if score > bestScore {
			best = info.port
			bestScore = score
		}
	}
	//t.core.log.Println("DEBUG: sending to", best, "bandwidth", getBandwidth(best))
	return best, uint64(myDist)
}

////////////////////////////////////////////////////////////////////////////////

//Signature stuff

type sigInfo struct {
	next sigPubKey
	sig  sigBytes
}

////////////////////////////////////////////////////////////////////////////////
