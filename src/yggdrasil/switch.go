package yggdrasil

// This part constructs a spanning tree of the network
// It routes packets based on distance on the spanning tree
//  In general, this is *not* equivalent to routing on the tree
//  It falls back to the tree in the worst case, but it can take shortcuts too
// This is the part that makse routing reasonably efficient on scale-free graphs

// TODO document/comment everything in a lot more detail

// TODO? use a pre-computed lookup table (python version had this)
//  A little annoying to do with constant changes from backpressure

import "time"
import "sort"
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
	time      time.Time     // Time this node was last seen
	firstSeen time.Time
	port      switchPort // Interface number of this peer
	msg       switchMsg  // The wire switchMsg used
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
	msg     *switchMsg
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

func (t *switchTable) getLocator() switchLocator {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.data.locator.clone()
}

func (t *switchTable) doMaintenance() {
	// Periodic maintenance work to keep things internally consistent
	t.mutex.Lock()         // Write lock
	defer t.mutex.Unlock() // Release lock when we're done
	t.cleanRoot()
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
		t.core.peers.sendSwitchMsgs()
	}
}

func (t *switchTable) removePeer(port switchPort) {
	delete(t.data.peers, port)
	t.updater.Store(&sync.Once{})
	// TODO if parent, find a new peer to use as parent instead
	for _, info := range t.data.peers {
		t.unlockedHandleMsg(&info.msg, info.port)
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

type switchMsg struct {
	Root   sigPubKey
	TStamp int64
	Hops   []switchMsgHop
}

type switchMsgHop struct {
	Port switchPort
	Next sigPubKey
	Sig  sigBytes
}

func (t *switchTable) getMsg() *switchMsg {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	if t.parent == 0 {
		return &switchMsg{Root: t.key, TStamp: t.data.locator.tstamp}
	} else if parent, isIn := t.data.peers[t.parent]; isIn {
		msg := parent.msg
		msg.Hops = append([]switchMsgHop(nil), msg.Hops...)
		return &msg
	} else {
		return nil
	}
}

func (t *switchTable) checkRoot(msg *switchMsg) bool {
	// returns false if it's a dropped root, not a better root, or has an older timestamp
	// returns true otherwise
	// used elsewhere to keep inserting peers into the dht only if root info is OK
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	dropTstamp, isIn := t.drop[msg.Root]
	switch {
	case isIn && dropTstamp >= msg.TStamp:
		return false
	case firstIsBetter(&msg.Root, &t.data.locator.root):
		return true
	case t.data.locator.root != msg.Root:
		return false
	case t.data.locator.tstamp > msg.TStamp:
		return false
	default:
		return true
	}
}

func (t *switchTable) handleMsg(msg *switchMsg, fromPort switchPort) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.unlockedHandleMsg(msg, fromPort)
}

func (t *switchTable) unlockedHandleMsg(msg *switchMsg, fromPort switchPort) {
	// TODO directly use a switchMsg instead of switchMessage + sigs
	now := time.Now()
	// Set up the sender peerInfo
	var sender peerInfo
	sender.locator.root = msg.Root
	sender.locator.tstamp = msg.TStamp
	prevKey := msg.Root
	for _, hop := range msg.Hops {
		// Build locator and signatures
		var sig sigInfo
		sig.next = hop.Next
		sig.sig = hop.Sig
		sender.locator.coords = append(sender.locator.coords, hop.Port)
		sender.key = prevKey
		prevKey = hop.Next
	}
	sender.msg = *msg
	oldSender, isIn := t.data.peers[fromPort]
	if !isIn {
		oldSender.firstSeen = now
	}
	sender.firstSeen = oldSender.firstSeen
	sender.port = fromPort
	sender.time = now
	// Decide what to do
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
	if !equiv(&sender.locator, &oldSender.locator) {
		doUpdate = true
		//sender.firstSeen = now // TODO? uncomment to prevent flapping?
	}
	t.data.peers[fromPort] = sender
	updateRoot := false
	oldParent, isIn := t.data.peers[t.parent]
	noParent := !isIn
	noLoop := func() bool {
		for idx := 0; idx < len(msg.Hops)-1; idx++ {
			if msg.Hops[idx].Next == t.core.sigPub {
				return false
			}
		}
		if sender.locator.root == t.core.sigPub {
			return false
		}
		return true
	}()
	sTime := now.Sub(sender.firstSeen)
	pTime := oldParent.time.Sub(oldParent.firstSeen) + switch_timeout
	// Really want to compare sLen/sTime and pLen/pTime
	// Cross multiplied to avoid divide-by-zero
	cost := len(sender.locator.coords) * int(pTime.Seconds())
	pCost := len(t.data.locator.coords) * int(sTime.Seconds())
	dropTstamp, isIn := t.drop[sender.locator.root]
	// Here be dragons
	switch {
	case !noLoop: // do nothing
	case isIn && dropTstamp >= sender.locator.tstamp: // do nothing
	case firstIsBetter(&sender.locator.root, &t.data.locator.root):
		updateRoot = true
	case t.data.locator.root != sender.locator.root: // do nothing
	case t.data.locator.tstamp > sender.locator.tstamp: // do nothing
	case noParent:
		updateRoot = true
	case cost < pCost:
		updateRoot = true
	case sender.port != t.parent: // do nothing
	case !equiv(&sender.locator, &t.data.locator):
		updateRoot = true
	case now.Sub(t.time) < switch_throttle: // do nothing
	case sender.locator.tstamp > t.data.locator.tstamp:
		updateRoot = true
	}
	if updateRoot {
		if !equiv(&sender.locator, &t.data.locator) {
			doUpdate = true
			t.data.seq++
			select {
			case t.core.router.reset <- struct{}{}:
			default:
			}
			//t.core.log.Println("Switch update:", msg.locator.root, msg.locator.tstamp, msg.locator.coords)
			//fmt.Println("Switch update:", msg.Locator.Root, msg.Locator.Tstamp, msg.Locator.Coords)
		}
		if t.data.locator.tstamp != sender.locator.tstamp {
			t.time = now
		}
		t.data.locator = sender.locator
		t.parent = sender.port
		//t.core.log.Println("Switch update:", msg.Locator.Root, msg.Locator.Tstamp, msg.Locator.Coords)
		t.core.peers.sendSwitchMsgs()
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
	sort.SliceStable(newTable.elems, func(i, j int) bool {
		return t.data.peers[newTable.elems[i].port].firstSeen.Before(t.data.peers[newTable.elems[j].port].firstSeen)
	})
	t.table.Store(newTable)
}

func (t *switchTable) lookup(dest []byte) switchPort {
	t.updater.Load().(*sync.Once).Do(t.updateTable)
	table := t.table.Load().(lookupTable)
	myDist := table.self.dist(dest)
	if myDist == 0 {
		return 0
	}
	// cost is in units of (expected distance) + (expected queue size), where expected distance is used as an approximation of the minimum backpressure gradient needed for packets to flow
	ports := t.core.peers.getPorts()
	var best switchPort
	bestCost := int64(^uint64(0) >> 1)
	for _, info := range table.elems {
		dist := info.locator.dist(dest)
		if !(dist < myDist) {
			continue
		}
		p, isIn := ports[info.port]
		if !isIn {
			continue
		}
		cost := int64(dist) + p.getQueueSize()
		if cost < bestCost {
			best = info.port
			bestCost = cost
		}
	}
	//t.core.log.Println("DEBUG: sending to", best, "cost", bestCost)
	return best
}

////////////////////////////////////////////////////////////////////////////////

//Signature stuff

type sigInfo struct {
	next sigPubKey
	sig  sigBytes
}

////////////////////////////////////////////////////////////////////////////////
