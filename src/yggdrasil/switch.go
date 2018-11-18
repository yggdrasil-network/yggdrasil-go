package yggdrasil

// This part constructs a spanning tree of the network
// It routes packets based on distance on the spanning tree
//  In general, this is *not* equivalent to routing on the tree
//  It falls back to the tree in the worst case, but it can take shortcuts too
// This is the part that makse routing reasonably efficient on scale-free graphs

// TODO document/comment everything in a lot more detail

// TODO? use a pre-computed lookup table (python version had this)
//  A little annoying to do with constant changes from backpressure

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

const switch_timeout = time.Minute
const switch_updateInterval = switch_timeout / 2
const switch_throttle = switch_updateInterval / 2
const switch_parent_threshold = time.Second

// The switch locator represents the topology and network state dependent info about a node, minus the signatures that go with it.
// Nodes will pick the best root they see, provided that the root continues to push out updates with new timestamps.
// The coords represent a path from the root to a node.
// This path is generally part of a spanning tree, except possibly the last hop (it can loop when sending coords to your parent, but they see this and know not to use a looping path).
type switchLocator struct {
	root   sigPubKey
	tstamp int64
	coords []switchPort
}

// Returns true if the first sigPubKey has a higher TreeID.
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

// Returns true if the this locator represents an ancestor of the locator given as an argument.
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
	key     sigPubKey     // ID of this peer
	locator switchLocator // Should be able to respond with signatures upon request
	degree  uint64        // Self-reported degree
	time    time.Time     // Time this node was last seen
	cost    time.Duration // Exponentially weighted average latency relative to the current parent, initialized to 1 hour
	port    switchPort    // Interface number of this peer
	msg     switchMsg     // The wire switchMsg used
}

// This is just a uint64 with a named type for clarity reasons.
type switchPort uint64

// This is the subset of the information about a peer needed to make routing decisions, and it stored separately in an atomically accessed table, which gets hammered in the "hot loop" of the routing logic (see: peer.handleTraffic in peers.go).
type tableElem struct {
	port    switchPort
	locator switchLocator
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
	core     *Core
	key      sigPubKey           // Our own key
	time     time.Time           // Time when locator.tstamp was last updated
	drop     map[sigPubKey]int64 // Tstamp associated with a dropped root
	mutex    sync.RWMutex        // Lock for reads/writes of switchData
	parent   switchPort          // Port of whatever peer is our parent, or self if we're root
	data     switchData          //
	updater  atomic.Value        // *sync.Once
	table    atomic.Value        // lookupTable
	packetIn chan []byte         // Incoming packets for the worker to handle
	idleIn   chan switchPort     // Incoming idle notifications from peer links
	admin    chan func()         // Pass a lambda for the admin socket to query stuff
	queues   switch_buffers      // Queues - not atomic so ONLY use through admin chan
}

// Initializes the switchTable struct.
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
	t.packetIn = make(chan []byte, 1024)
	t.idleIn = make(chan switchPort, 1024)
	t.admin = make(chan func())
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
			select {
			case t.core.router.reset <- struct{}{}:
			default:
			}
		}
		t.data.locator = switchLocator{root: t.key, tstamp: now.Unix()}
		t.core.peers.sendSwitchMsgs()
	}
}

// Removes a peer.
// Must be called by the router mainLoop goroutine, e.g. call router.doAdmin with a lambda that calls this.
// If the removed peer was this node's parent, it immediately tries to find a new parent.
func (t *switchTable) forgetPeer(port switchPort) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	delete(t.data.peers, port)
	t.updater.Store(&sync.Once{})
	if port != t.parent {
		return
	}
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
	Root   sigPubKey
	TStamp int64
	Hops   []switchMsgHop
}

// This represents the signed information about the path leading from the root the Next node, via the Port specified here.
type switchMsgHop struct {
	Port switchPort
	Next sigPubKey
	Sig  sigBytes
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
// It's very important to not change the order of the statements in the case function unless you're absolutely sure that it's safe, including safe if used along side nodes that used the previous order.
func (t *switchTable) unlockedHandleMsg(msg *switchMsg, fromPort switchPort, replace bool) {
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
	oldSender, isIn := t.data.peers[fromPort]
	if !isIn || oldSender.locator.root != msg.Root {
		// Reset the cost
		sender.cost = 0
	} else if sender.locator.tstamp > oldSender.locator.tstamp {
		var lag time.Duration
		if sender.locator.tstamp > t.data.locator.tstamp {
			// Latency based on how early the last message arrived before the parent's
			lag = oldSender.time.Sub(t.time)
		} else {
			// Waiting this long cost us something
			lag = now.Sub(t.time)
		}
		// Limit how much lag can affect things from a single packet
		if lag > switch_parent_threshold/8 {
			lag = switch_parent_threshold / 8
		}
		if lag < -switch_parent_threshold/8 {
			lag = -switch_parent_threshold / 8
		}
		sender.cost += lag
		// Limit how much the cost can move in total
		if sender.cost < -switch_parent_threshold {
			sender.cost = -switch_parent_threshold
		}
		if sender.cost > switch_parent_threshold {
			sender.cost = switch_parent_threshold
		}
		if sender.port == t.parent {
			// But always reset the parent's cost to 0, by definition
			sender.cost = 0
		}
	}
	if !equiv(&sender.locator, &oldSender.locator) {
		doUpdate = true
		// Penalize flappy routes by resetting cost
		sender.cost = 0
	}
	t.data.peers[fromPort] = sender
	updateRoot := false
	_, isIn = t.data.peers[t.parent]
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
	// Here be dragons
	switch {
	case !noLoop:
		// This route loops, so we can't use the sender as our parent.
	case isIn && dropTstamp >= sender.locator.tstamp:
		// This is a known root with a timestamp older than a known timeout, so we can't trust it not to be a replay.
	case firstIsBetter(&sender.locator.root, &t.data.locator.root):
		// This is a better root than what we're currently using, so we should update.
		updateRoot = true
	case t.data.locator.root != sender.locator.root:
		// This is not the same root, and it's apparently not better (from the above), so we should ignore it.
	case t.data.locator.tstamp > sender.locator.tstamp:
		// This timetsamp is older than the most recently seen one from this root, so we should ignore it.
	case noParent:
		// We currently have no working parent, so update.
		updateRoot = true
	case sender.cost <= -switch_parent_threshold:
		// Cumulatively faster by a significant margin.
		updateRoot = true
	case sender.port != t.parent:
		// Ignore further cases if the sender isn't our parent.
	case !equiv(&sender.locator, &t.data.locator):
		// Special case
		// If coords changed, then this may now be a worse parent than before
		// Re-parent the node (de-parent and reprocess the message)
		// Then reprocess *all* messages to look for a better parent
		// This is so we don't keep using this node as our parent if there's something better
		t.parent = 0
		t.unlockedHandleMsg(msg, fromPort, true)
		for _, info := range t.data.peers {
			t.unlockedHandleMsg(&info.msg, info.port, true)
		}
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
			for port, peer := range t.data.peers {
				peer.cost = 0
				t.data.peers[port] = peer
			}
			select {
			case t.core.router.reset <- struct{}{}:
			default:
			}
		}
		if t.data.locator.tstamp != sender.locator.tstamp {
			t.time = now
		}
		t.data.locator = sender.locator
		t.parent = sender.port
		t.core.peers.sendSwitchMsgs()
	}
	if doUpdate {
		t.updater.Store(&sync.Once{})
	}
	return
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
	t.core.log.Println("Starting switch")
	go t.doWorker()
	return nil
}

// Check if a packet should go to the self node
// This means there's no node closer to the destination than us
// This is mainly used to identify packets addressed to us, or that hit a blackhole
func (t *switchTable) selfIsClosest(dest []byte) bool {
	table := t.getTable()
	myDist := table.self.dist(dest)
	if myDist == 0 {
		// Skip the iteration step if it's impossible to be closer
		return true
	}
	for _, info := range table.elems {
		dist := info.locator.dist(dest)
		if dist < myDist {
			return false
		}
	}
	return true
}

// Returns true if the peer is closer to the destination than ourself
func (t *switchTable) portIsCloser(dest []byte, port switchPort) bool {
	table := t.getTable()
	if info, isIn := table.elems[port]; isIn {
		theirDist := info.locator.dist(dest)
		myDist := table.self.dist(dest)
		return theirDist < myDist
	} else {
		return false
	}
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
func (t *switchTable) handleIn(packet []byte, idle map[switchPort]struct{}) bool {
	coords := switch_getPacketCoords(packet)
	ports := t.core.peers.getPorts()
	if t.selfIsClosest(coords) {
		// TODO? call the router directly, and remove the whole concept of a self peer?
		ports[0].sendPacket(packet)
		return true
	}
	table := t.getTable()
	myDist := table.self.dist(coords)
	var best *peer
	bestDist := myDist
	for port := range idle {
		if to := ports[port]; to != nil {
			if info, isIn := table.elems[to.port]; isIn {
				dist := info.locator.dist(coords)
				if !(dist < bestDist) {
					continue
				}
				best = to
				bestDist = dist
			}
		}
	}
	if best != nil {
		// Send to the best idle next hop
		delete(idle, best.port)
		best.sendPacket(packet)
		return true
	} else {
		// Didn't find anyone idle to send it to
		return false
	}
}

// Info about a buffered packet
type switch_packetInfo struct {
	bytes []byte
	time  time.Time // Timestamp of when the packet arrived
}

const switch_buffer_maxSize = 4 * 1048576 // Maximum 4 MB

// Used to keep track of buffered packets
type switch_buffer struct {
	packets []switch_packetInfo // Currently buffered packets, which may be dropped if it grows too large
	size    uint64              // Total queue size in bytes
}

type switch_buffers struct {
	bufs    map[string]switch_buffer // Buffers indexed by StreamID
	size    uint64                   // Total size of all buffers, in bytes
	maxbufs int
	maxsize uint64
}

func (b *switch_buffers) cleanup(t *switchTable) {
	for streamID, buf := range b.bufs {
		// Remove queues for which we have no next hop
		packet := buf.packets[0]
		coords := switch_getPacketCoords(packet.bytes)
		if t.selfIsClosest(coords) {
			for _, packet := range buf.packets {
				util_putBytes(packet.bytes)
			}
			b.size -= buf.size
			delete(b.bufs, streamID)
		}
	}

	for b.size > switch_buffer_maxSize {
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
			util_putBytes(packet.bytes)
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
func (t *switchTable) handleIdle(port switchPort) bool {
	to := t.core.peers.getPorts()[port]
	if to == nil {
		return true
	}
	var best string
	var bestPriority float64
	t.queues.cleanup(t)
	now := time.Now()
	for streamID, buf := range t.queues.bufs {
		// Filter over the streams that this node is closer to
		// Keep the one with the smallest queue
		packet := buf.packets[0]
		coords := switch_getPacketCoords(packet.bytes)
		priority := float64(now.Sub(packet.time)) / float64(buf.size)
		if priority > bestPriority && t.portIsCloser(coords, port) {
			best = streamID
			bestPriority = priority
		}
	}
	if bestPriority != 0 {
		buf := t.queues.bufs[best]
		var packet switch_packetInfo
		// TODO decide if this should be LIFO or FIFO
		packet, buf.packets = buf.packets[0], buf.packets[1:]
		buf.size -= uint64(len(packet.bytes))
		t.queues.size -= uint64(len(packet.bytes))
		if len(buf.packets) == 0 {
			delete(t.queues.bufs, best)
		} else {
			// Need to update the map, since buf was retrieved by value
			t.queues.bufs[best] = buf
		}
		to.sendPacket(packet.bytes)
		return true
	} else {
		return false
	}
}

// The switch worker does routing lookups and sends packets to where they need to be
func (t *switchTable) doWorker() {
	t.queues.bufs = make(map[string]switch_buffer) // Packets per PacketStreamID (string)
	idle := make(map[switchPort]struct{})          // this is to deduplicate things
	for {
		select {
		case bytes := <-t.packetIn:
			// Try to send it somewhere (or drop it if it's corrupt or at a dead end)
			if !t.handleIn(bytes, idle) {
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
				t.queues.cleanup(t)
			}
		case port := <-t.idleIn:
			// Try to find something to send to this peer
			if !t.handleIdle(port) {
				// Didn't find anything ready to send yet, so stay idle
				idle[port] = struct{}{}
			}
		case f := <-t.admin:
			f()
		}
	}
}

// Passed a function to call.
// This will send the function to t.admin and block until it finishes.
func (t *switchTable) doAdmin(f func()) {
	// Pass this a function that needs to be run by the router's main goroutine
	// It will pass the function to the router and wait for the router to finish
	done := make(chan struct{})
	newF := func() {
		f()
		close(done)
	}
	t.admin <- newF
	<-done
}
