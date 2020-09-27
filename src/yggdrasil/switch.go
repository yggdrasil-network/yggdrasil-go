package yggdrasil

// This part constructs a spanning tree of the network
// It routes packets based on distance on the spanning tree
//  In general, this is *not* equivalent to routing on the tree
//  It falls back to the tree in the worst case, but it can take shortcuts too
// This is the part that makes routing reasonably efficient on scale-free graphs

// TODO document/comment everything in a lot more detail

// TODO? use a pre-computed lookup table (python version had this)
//  A little annoying to do with constant changes from backpressure

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"

	"github.com/Arceliar/phony"
)

const (
	switch_timeout          = time.Minute
	switch_updateInterval   = switch_timeout / 2
	switch_throttle         = switch_updateInterval / 2
	switch_faster_threshold = 240 //Number of switch updates before switching to a faster parent
)

// The switch locator represents the topology and network state dependent info about a node, minus the signatures that go with it.
// Nodes will pick the best root they see, provided that the root continues to push out updates with new timestamps.
// The coords represent a path from the root to a node.
// This path is generally part of a spanning tree, except possibly the last hop (it can loop when sending coords to your parent, but they see this and know not to use a looping path).
type switchLocator struct {
	root   crypto.SigPubKey
	tstamp int64
	coords []switchPort
}

// Returns true if the first sigPubKey has a higher TreeID.
func firstIsBetter(first, second *crypto.SigPubKey) bool {
	// Higher TreeID is better
	ftid := crypto.GetTreeID(first)
	stid := crypto.GetTreeID(second)
	for idx := 0; idx < len(ftid); idx++ {
		if ftid[idx] == stid[idx] {
			continue
		}
		return ftid[idx] > stid[idx]
	}
	// Edge case, when comparing identical IDs
	return false
}

// Returns a copy of the locator which can safely be mutated.
func (l *switchLocator) clone() switchLocator {
	// Used to create a deep copy for use in messages
	// Copy required because we need to mutate coords before sending
	// (By appending the port from us to the destination)
	loc := *l
	loc.coords = make([]switchPort, len(l.coords), len(l.coords)+1)
	copy(loc.coords, l.coords)
	return loc
}

// Gets the distance a locator is from the provided destination coords, with the coords provided in []byte format (used to compress integers sent over the wire).
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

// Gets coords in wire encoded format, with *no* length prefix.
func (l *switchLocator) getCoords() []byte {
	bs := make([]byte, 0, len(l.coords))
	for _, coord := range l.coords {
		c := wire_encode_uint64(uint64(coord))
		bs = append(bs, c...)
	}
	return bs
}

// Returns true if this locator represents an ancestor of the locator given as an argument.
// Ancestor means that it's the parent node, or the parent of parent, and so on...
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

// Information about a peer, used by the switch to build the tree and eventually make routing decisions.
type peerInfo struct {
	key     crypto.SigPubKey      // ID of this peer
	locator switchLocator         // Should be able to respond with signatures upon request
	degree  uint64                // Self-reported degree
	time    time.Time             // Time this node was last seen
	faster  map[switchPort]uint64 // Counter of how often a node is faster than the current parent, penalized extra if slower
	port    switchPort            // Interface number of this peer
	msg     switchMsg             // The wire switchMsg used
	blocked bool                  // True if the link is blocked, used to avoid parenting a blocked link
}

// This is just a uint64 with a named type for clarity reasons.
type switchPort uint64

// This is the subset of the information about a peer needed to make routing decisions, and it stored separately in an atomically accessed table, which gets hammered in the "hot loop" of the routing logic (see: peer.handleTraffic in peers.go).
type tableElem struct {
	port    switchPort
	locator switchLocator
	time    time.Time
}

// This is the subset of the information about all peers needed to make routing decisions, and it stored separately in an atomically accessed table, which gets hammered in the "hot loop" of the routing logic (see: peer.handleTraffic in peers.go).
type lookupTable struct {
	self  switchLocator
	elems map[switchPort]tableElem
}

// This is switch information which is mutable and needs to be modified by other goroutines, but is not accessed atomically.
// Use the switchTable functions to access it safely using the RWMutex for synchronization.
type switchData struct {
	// All data that's mutable and used by exported Table methods
	// To be read/written with atomic.Value Store/Load calls
	locator switchLocator
	seq     uint64 // Sequence number, reported to peers, so they know about changes
	peers   map[switchPort]peerInfo
	msg     *switchMsg
}

// All the information stored by the switch.
type switchTable struct {
	core        *Core
	key         crypto.SigPubKey           // Our own key
	time        time.Time                  // Time when locator.tstamp was last updated
	drop        map[crypto.SigPubKey]int64 // Tstamp associated with a dropped root
	mutex       sync.RWMutex               // Lock for reads/writes of switchData
	parent      switchPort                 // Port of whatever peer is our parent, or self if we're root
	data        switchData                 //
	updater     atomic.Value               // *sync.Once
	table       atomic.Value               // lookupTable
	phony.Inbox                            // Owns the below
	queues      switch_buffers             // Queues - not atomic so ONLY use through the actor
	idle        map[switchPort]struct{}    // idle peers - not atomic so ONLY use through the actor
	sending     map[switchPort]struct{}    // peers known to be blocked in a send (somehow)
}

// Minimum allowed total size of switch queues.
const SwitchQueueTotalMinSize = 4 * 1024 * 1024

// Initializes the switchTable struct.
func (t *switchTable) init(core *Core) {
	now := time.Now()
	t.core = core
	t.key = t.core.sigPub
	locator := switchLocator{root: t.key, tstamp: now.Unix()}
	peers := make(map[switchPort]peerInfo)
	t.data = switchData{locator: locator, peers: peers}
	t.updater.Store(&sync.Once{})
	t.table.Store(lookupTable{})
	t.drop = make(map[crypto.SigPubKey]int64)
	phony.Block(t, func() {
		core.config.Mutex.RLock()
		if core.config.Current.SwitchOptions.MaxTotalQueueSize > SwitchQueueTotalMinSize {
			t.queues.totalMaxSize = core.config.Current.SwitchOptions.MaxTotalQueueSize
		} else {
			t.queues.totalMaxSize = SwitchQueueTotalMinSize
		}
		core.config.Mutex.RUnlock()
		t.queues.bufs = make(map[string]switch_buffer)
		t.idle = make(map[switchPort]struct{})
		t.sending = make(map[switchPort]struct{})
	})
}

func (t *switchTable) reconfigure() {
	// This is where reconfiguration would go, if we had anything useful to do.
	t.core.link.reconfigure()
	t.core.peers.reconfigure()
}

// Safely gets a copy of this node's locator.
func (t *switchTable) getLocator() switchLocator {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.data.locator.clone()
}

// Regular maintenance to possibly timeout/reset the root and similar.
func (t *switchTable) doMaintenance() {
	// Periodic maintenance work to keep things internally consistent
	t.mutex.Lock()         // Write lock
	defer t.mutex.Unlock() // Release lock when we're done
	t.cleanRoot()
	t.cleanDropped()
}

// Updates the root periodically if it is ourself, or promotes ourself to root if we're better than the current root or if the current root has timed out.
func (t *switchTable) cleanRoot() {
	// TODO rethink how this is done?...
	// Get rid of the root if it looks like its timed out
	now := time.Now()
	doUpdate := false
	if now.Sub(t.time) > switch_timeout {
		dropped := t.data.peers[t.parent]
		dropped.time = t.time
		t.drop[t.data.locator.root] = t.data.locator.tstamp
		doUpdate = true
	}
	// Or, if we're better than our root, root ourself
	if firstIsBetter(&t.key, &t.data.locator.root) {
		doUpdate = true
	}
	// Or, if we are the root, possibly update our timestamp
	if t.data.locator.root == t.key &&
		now.Sub(t.time) > switch_updateInterval {
		doUpdate = true
	}
	if doUpdate {
		t.parent = switchPort(0)
		t.time = now
		if t.data.locator.root != t.key {
			t.data.seq++
			t.updater.Store(&sync.Once{})
			t.core.router.reset(nil)
		}
		t.data.locator = switchLocator{root: t.key, tstamp: now.Unix()}
		t.core.peers.sendSwitchMsgs(t)
	}
}

// Blocks and, if possible, unparents a peer
func (t *switchTable) blockPeer(port switchPort) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	peer, isIn := t.data.peers[port]
	if !isIn {
		return
	}
	peer.blocked = true
	t.data.peers[port] = peer
	if port != t.parent {
		return
	}
	t.parent = 0
	for _, info := range t.data.peers {
		if info.port == port {
			continue
		}
		t.unlockedHandleMsg(&info.msg, info.port, true)
	}
	t.unlockedHandleMsg(&peer.msg, peer.port, true)
}

// Removes a peer.
// Must be called by the router actor with a lambda that calls this.
// If the removed peer was this node's parent, it immediately tries to find a new parent.
func (t *switchTable) forgetPeer(port switchPort) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	delete(t.data.peers, port)
	t.updater.Store(&sync.Once{})
	if port != t.parent {
		return
	}
	t.parent = 0
	for _, info := range t.data.peers {
		t.unlockedHandleMsg(&info.msg, info.port, true)
	}
}

// Dropped is a list of roots that are better than the current root, but stopped sending new timestamps.
// If we switch to a new root, and that root is better than an old root that previously timed out, then we can clean up the old dropped root infos.
// This function is called periodically to do that cleanup.
func (t *switchTable) cleanDropped() {
	// TODO? only call this after root changes, not periodically
	for root := range t.drop {
		if !firstIsBetter(&root, &t.data.locator.root) {
			delete(t.drop, root)
		}
	}
}

// A switchMsg contains the root node's sig key, timestamp, and signed per-hop information about a path from the root node to some other node in the network.
// This is exchanged with peers to construct the spanning tree.
// A subset of this information, excluding the signatures, is used to construct locators that are used elsewhere in the code.
type switchMsg struct {
	Root   crypto.SigPubKey
	TStamp int64
	Hops   []switchMsgHop
}

// This represents the signed information about the path leading from the root the Next node, via the Port specified here.
type switchMsgHop struct {
	Port switchPort
	Next crypto.SigPubKey
	Sig  crypto.SigBytes
}

// This returns a *switchMsg to a copy of this node's current switchMsg, which can safely have additional information appended to Hops and sent to a peer.
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

// This function checks that the root information in a switchMsg is OK.
// In particular, that the root is better, or else the same as the current root but with a good timestamp, and that this root+timestamp haven't been dropped due to timeout.
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

// This is a mutexed wrapper to unlockedHandleMsg, and is called by the peer structs in peers.go to pass a switchMsg for that peer into the switch.
func (t *switchTable) handleMsg(msg *switchMsg, fromPort switchPort) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.unlockedHandleMsg(msg, fromPort, false)
}

// This updates the switch with information about a peer.
// Then the tricky part, it decides if it should update our own locator as a result.
// That happens if this node is already our parent, or is advertising a better root, or is advertising a better path to the same root, etc...
// There are a lot of very delicate order sensitive checks here, so its' best to just read the code if you need to understand what it's doing.
// It's very important to not change the order of the statements in the case function unless you're absolutely sure that it's safe, including safe if used alongside nodes that used the previous order.
// Set the third arg to true if you're reprocessing an old message, e.g. to find a new parent after one disconnects, to avoid updating some timing related things.
func (t *switchTable) unlockedHandleMsg(msg *switchMsg, fromPort switchPort, reprocessing bool) {
	// TODO directly use a switchMsg instead of switchMessage + sigs
	now := time.Now()
	// Set up the sender peerInfo
	var sender peerInfo
	sender.locator.root = msg.Root
	sender.locator.tstamp = msg.TStamp
	prevKey := msg.Root
	for _, hop := range msg.Hops {
		// Build locator
		sender.locator.coords = append(sender.locator.coords, hop.Port)
		sender.key = prevKey
		prevKey = hop.Next
	}
	sender.msg = *msg
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
	oldSender := t.data.peers[fromPort]
	if !equiv(&sender.locator, &oldSender.locator) {
		// Reset faster info, we'll start refilling it right after this
		sender.faster = nil
		doUpdate = true
	}
	// Update the matrix of peer "faster" thresholds
	if reprocessing {
		sender.faster = oldSender.faster
		sender.time = oldSender.time
		sender.blocked = oldSender.blocked
	} else {
		sender.faster = make(map[switchPort]uint64, len(oldSender.faster))
		for port, peer := range t.data.peers {
			if port == fromPort {
				continue
			} else if sender.locator.root != peer.locator.root || sender.locator.tstamp > peer.locator.tstamp {
				// We were faster than this node, so increment, as long as we don't overflow because of it
				if oldSender.faster[peer.port] < switch_faster_threshold {
					sender.faster[port] = oldSender.faster[peer.port] + 1
				} else {
					sender.faster[port] = switch_faster_threshold
				}
			} else {
				// Slower than this node, penalize (more than the reward amount)
				if oldSender.faster[port] > 1 {
					sender.faster[port] = oldSender.faster[peer.port] - 2
				} else {
					sender.faster[port] = 0
				}
			}
		}
	}
	// Update sender
	t.data.peers[fromPort] = sender
	// Decide if we should also update our root info to make the sender our parent
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
	dropTstamp, isIn := t.drop[sender.locator.root]
	// Decide if we need to update info about the root or change parents.
	switch {
	case !noLoop:
		// This route loops, so we can't use the sender as our parent.
	case isIn && dropTstamp >= sender.locator.tstamp:
		// This is a known root with a timestamp older than a known timeout, so we can't trust it to be a new announcement.
	case firstIsBetter(&sender.locator.root, &t.data.locator.root):
		// This is a better root than what we're currently using, so we should update.
		updateRoot = true
	case t.data.locator.root != sender.locator.root:
		// This is not the same root, and it's apparently not better (from the above), so we should ignore it.
	case t.data.locator.tstamp > sender.locator.tstamp:
		// This timetsamp is older than the most recently seen one from this root, so we should ignore it.
	case noParent:
		// We currently have no working parent, and at this point in the switch statement, anything is better than nothing.
		updateRoot = true
	case sender.faster[t.parent] >= switch_faster_threshold:
		// The is reliably faster than the current parent.
		updateRoot = true
	case !sender.blocked && oldParent.blocked:
		// Replace a blocked parent
		updateRoot = true
	case reprocessing && sender.blocked && !oldParent.blocked:
		// Don't replace an unblocked parent when reprocessing
	case reprocessing && sender.faster[t.parent] > oldParent.faster[sender.port]:
		// The sender seems to be reliably faster than the current parent, so switch to them instead.
		updateRoot = true
	case sender.port != t.parent:
		// Ignore further cases if the sender isn't our parent.
	case !reprocessing && !equiv(&sender.locator, &t.data.locator):
		// Special case:
		// If coords changed, then we need to penalize this node somehow, to prevent flapping.
		// First, reset all faster-related info to 0.
		// Then, de-parent the node and reprocess all messages to find a new parent.
		t.parent = 0
		for _, peer := range t.data.peers {
			if peer.port == sender.port {
				continue
			}
			t.unlockedHandleMsg(&peer.msg, peer.port, true)
		}
		// Process the sender last, to avoid keeping them as a parent if at all possible.
		t.unlockedHandleMsg(&sender.msg, sender.port, true)
	case now.Sub(t.time) < switch_throttle:
		// We've already gotten an update from this root recently, so ignore this one to avoid flooding.
	case sender.locator.tstamp > t.data.locator.tstamp:
		// The timestamp was updated, so we need to update locally and send to our peers.
		updateRoot = true
	}
	if updateRoot {
		if !equiv(&sender.locator, &t.data.locator) {
			doUpdate = true
			t.data.seq++
			t.core.router.reset(nil)
		}
		if t.data.locator.tstamp != sender.locator.tstamp {
			t.time = now
		}
		t.data.locator = sender.locator
		t.parent = sender.port
		t.core.peers.sendSwitchMsgs(t)
	}
	if true || doUpdate {
		t.updater.Store(&sync.Once{})
	}
}

////////////////////////////////////////////////////////////////////////////////

// The rest of these are related to the switch worker

// This is called via a sync.Once to update the atomically readable subset of switch information that gets used for routing decisions.
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
		elems: make(map[switchPort]tableElem, len(t.data.peers)),
	}
	for _, pinfo := range t.data.peers {
		//if !pinfo.forward { continue }
		if pinfo.locator.root != newTable.self.root {
			continue
		}
		loc := pinfo.locator.clone()
		loc.coords = loc.coords[:len(loc.coords)-1] // Remove the them->self link
		newTable.elems[pinfo.port] = tableElem{
			locator: loc,
			port:    pinfo.port,
			time:    pinfo.time,
		}
	}
	t.table.Store(newTable)
}

// Returns a copy of the atomically-updated table used for switch lookups
func (t *switchTable) getTable() lookupTable {
	t.updater.Load().(*sync.Once).Do(t.updateTable)
	return t.table.Load().(lookupTable)
}

// Starts the switch worker
func (t *switchTable) start() error {
	t.core.log.Infoln("Starting switch")
	// There's actually nothing to do to start it...
	return nil
}

type closerInfo struct {
	elem tableElem
	dist int
}

// Return a map of ports onto distance, keeping only ports closer to the destination than this node
// If the map is empty (or nil), then no peer is closer
func (t *switchTable) getCloser(dest []byte) []closerInfo {
	table := t.getTable()
	myDist := table.self.dist(dest)
	if myDist == 0 {
		// Skip the iteration step if it's impossible to be closer
		return nil
	}
	t.queues.closer = t.queues.closer[:0]
	for _, info := range table.elems {
		dist := info.locator.dist(dest)
		if dist < myDist {
			t.queues.closer = append(t.queues.closer, closerInfo{info, dist})
		}
	}
	return t.queues.closer
}

// Returns true if the peer is closer to the destination than ourself
func (t *switchTable) portIsCloser(dest []byte, port switchPort) bool {
	table := t.getTable()
	if info, isIn := table.elems[port]; isIn {
		theirDist := info.locator.dist(dest)
		myDist := table.self.dist(dest)
		return theirDist < myDist
	}
	return false
}

// Get the coords of a packet without decoding
func switch_getPacketCoords(packet []byte) []byte {
	_, pTypeLen := wire_decode_uint64(packet)
	coords, _ := wire_decode_coords(packet[pTypeLen:])
	return coords
}

// Returns a unique string for each stream of traffic
// Equal to coords
// The sender may append arbitrary info to the end of coords (as long as it's begins with a 0x00) to designate separate traffic streams
// Currently, it's the IPv6 next header type and the first 2 uint16 of the next header
// This is equivalent to the TCP/UDP protocol numbers and the source / dest ports
// TODO figure out if something else would make more sense (other transport protocols?)
func switch_getPacketStreamID(packet []byte) string {
	return string(switch_getPacketCoords(packet))
}

// Returns the flowlabel from a given set of coords
func switch_getFlowLabelFromCoords(in []byte) []byte {
	for i, v := range in {
		if v == 0 {
			return in[i+1:]
		}
	}
	return []byte{}
}

// Find the best port for a given set of coords
func (t *switchTable) bestPortForCoords(coords []byte) switchPort {
	table := t.getTable()
	var best switchPort
	bestDist := table.self.dist(coords)
	for to, elem := range table.elems {
		dist := elem.locator.dist(coords)
		if !(dist < bestDist) {
			continue
		}
		best = to
		bestDist = dist
	}
	return best
}

// Handle an incoming packet
// Either send it to ourself, or to the first idle peer that's free
// Returns true if the packet has been handled somehow, false if it should be queued
func (t *switchTable) _handleIn(packet []byte, idle map[switchPort]struct{}, sending map[switchPort]struct{}) bool {
	coords := switch_getPacketCoords(packet)
	closer := t.getCloser(coords)
	if len(closer) == 0 {
		// TODO? call the router directly, and remove the whole concept of a self peer?
		self := t.core.peers.getPorts()[0]
		self.sendPacketsFrom(t, [][]byte{packet})
		return true
	}
	var best *closerInfo
	ports := t.core.peers.getPorts()
	for _, cinfo := range closer {
		to := ports[cinfo.elem.port]
		//_, isIdle := idle[cinfo.elem.port]
		_, isSending := sending[cinfo.elem.port]
		var update bool
		switch {
		case to == nil:
			// no port was found, ignore it
		case isSending:
			// the port is busy, ignore it
		case best == nil:
			// this is the first idle port we've found, so select it until we find a
			// better candidate port to use instead
			update = true
		case cinfo.dist < best.dist:
			// the port takes a shorter path/is more direct than our current
			// candidate, so select that instead
			update = true
		case cinfo.dist > best.dist:
			// the port takes a longer path/is less direct than our current candidate,
			// ignore it
		case cinfo.elem.locator.tstamp > best.elem.locator.tstamp:
			// has a newer tstamp from the root, so presumably a better path
			update = true
		case cinfo.elem.locator.tstamp < best.elem.locator.tstamp:
			// has a n older tstamp, so presumably a worse path
		case cinfo.elem.time.Before(best.elem.time):
			// same tstamp, but got it earlier, so presumably a better path
			//t.core.log.Println("DEBUG new best:", best.elem.time, cinfo.elem.time)
			update = true
		default:
			// the search for a port has finished
		}
		if update {
			b := cinfo // because cinfo gets mutated by the iteration
			best = &b
		}
	}
	if best != nil {
		if _, isIdle := idle[best.elem.port]; isIdle {
			delete(idle, best.elem.port)
			ports[best.elem.port].sendPacketsFrom(t, [][]byte{packet})
			return true
		}
	}
	// Didn't find anyone idle to send it to
	return false
}

// Info about a buffered packet
type switch_packetInfo struct {
	bytes []byte
	time  time.Time // Timestamp of when the packet arrived
}

// Used to keep track of buffered packets
type switch_buffer struct {
	packets []switch_packetInfo // Currently buffered packets, which may be dropped if it grows too large
	size    uint64              // Total queue size in bytes
}

type switch_buffers struct {
	totalMaxSize uint64
	bufs         map[string]switch_buffer // Buffers indexed by StreamID
	size         uint64                   // Total size of all buffers, in bytes
	maxbufs      int
	maxsize      uint64
	closer       []closerInfo // Scratch space
}

func (b *switch_buffers) _cleanup(t *switchTable) {
	for streamID, buf := range b.bufs {
		// Remove queues for which we have no next hop
		packet := buf.packets[0]
		coords := switch_getPacketCoords(packet.bytes)
		if len(t.getCloser(coords)) == 0 {
			for _, packet := range buf.packets {
				util.PutBytes(packet.bytes)
			}
			b.size -= buf.size
			delete(b.bufs, streamID)
		}
	}

	for b.size > b.totalMaxSize {
		// Drop a random queue
		target := rand.Uint64() % b.size
		var size uint64 // running total
		for streamID, buf := range b.bufs {
			size += buf.size
			if size < target {
				continue
			}
			var packet switch_packetInfo
			packet, buf.packets = buf.packets[0], buf.packets[1:]
			buf.size -= uint64(len(packet.bytes))
			b.size -= uint64(len(packet.bytes))
			util.PutBytes(packet.bytes)
			if len(buf.packets) == 0 {
				delete(b.bufs, streamID)
			} else {
				// Need to update the map, since buf was retrieved by value
				b.bufs[streamID] = buf
			}
			break
		}
	}
}

// Handles incoming idle notifications
// Loops over packets and sends the newest one that's OK for this peer to send
// Returns true if the peer is no longer idle, false if it should be added to the idle list
func (t *switchTable) _handleIdle(port switchPort) bool {
	// TODO? only send packets for which this is the best next hop that isn't currently blocked sending
	to := t.core.peers.getPorts()[port]
	if to == nil {
		return true
	}
	var packets [][]byte
	var psize int
	t.queues._cleanup(t)
	now := time.Now()
	for psize < 65535 {
		var best *string
		var bestPriority float64
		for streamID, buf := range t.queues.bufs {
			// Filter over the streams that this node is closer to
			// Keep the one with the smallest queue
			packet := buf.packets[0]
			coords := switch_getPacketCoords(packet.bytes)
			priority := float64(now.Sub(packet.time)) / float64(buf.size)
			if priority >= bestPriority && t.portIsCloser(coords, port) {
				b := streamID // copy since streamID is mutated in the loop
				best = &b
				bestPriority = priority
			}
		}
		if best != nil {
			buf := t.queues.bufs[*best]
			var packet switch_packetInfo
			// TODO decide if this should be LIFO or FIFO
			packet, buf.packets = buf.packets[0], buf.packets[1:]
			buf.size -= uint64(len(packet.bytes))
			t.queues.size -= uint64(len(packet.bytes))
			if len(buf.packets) == 0 {
				delete(t.queues.bufs, *best)
			} else {
				// Need to update the map, since buf was retrieved by value
				t.queues.bufs[*best] = buf
			}
			packets = append(packets, packet.bytes)
			psize += len(packet.bytes)
		} else {
			// Finished finding packets
			break
		}
	}
	if len(packets) > 0 {
		to.sendPacketsFrom(t, packets)
		return true
	}
	return false
}

func (t *switchTable) packetInFrom(from phony.Actor, bytes []byte) {
	t.Act(from, func() {
		t._packetIn(bytes)
	})
}

func (t *switchTable) _packetIn(bytes []byte) {
	// Try to send it somewhere (or drop it if it's corrupt or at a dead end)
	if !t._handleIn(bytes, t.idle, t.sending) {
		// There's nobody free to take it right now, so queue it for later
		packet := switch_packetInfo{bytes, time.Now()}
		streamID := switch_getPacketStreamID(packet.bytes)
		buf, bufExists := t.queues.bufs[streamID]
		buf.packets = append(buf.packets, packet)
		buf.size += uint64(len(packet.bytes))
		t.queues.size += uint64(len(packet.bytes))
		// Keep a track of the max total queue size
		if t.queues.size > t.queues.maxsize {
			t.queues.maxsize = t.queues.size
		}
		t.queues.bufs[streamID] = buf
		if !bufExists {
			// Keep a track of the max total queue count. Only recalculate this
			// when the queue is new because otherwise repeating len(dict) might
			// cause unnecessary processing overhead
			if len(t.queues.bufs) > t.queues.maxbufs {
				t.queues.maxbufs = len(t.queues.bufs)
			}
		}
		t.queues._cleanup(t)
	}
}

func (t *switchTable) _idleIn(port switchPort) {
	// Try to find something to send to this peer
	delete(t.sending, port)
	if !t._handleIdle(port) {
		// Didn't find anything ready to send yet, so stay idle
		t.idle[port] = struct{}{}
	}
}

func (t *switchTable) _sendingIn(port switchPort) {
	if _, isIn := t.idle[port]; !isIn {
		t.sending[port] = struct{}{}
	}
}
