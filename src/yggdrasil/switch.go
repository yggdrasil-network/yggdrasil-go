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
	"sync"
	"sync/atomic"
	"time"
)

const switch_timeout = time.Minute
const switch_updateInterval = switch_timeout / 2
const switch_throttle = switch_updateInterval / 2

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
	key       sigPubKey     // ID of this peer
	locator   switchLocator // Should be able to respond with signatures upon request
	degree    uint64        // Self-reported degree
	time      time.Time     // Time this node was last seen
	firstSeen time.Time
	port      switchPort // Interface number of this peer
	msg       switchMsg  // The wire switchMsg used
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
	parent   switchPort          // Port of whatever peer is our parent, or self if we're root
	drop     map[sigPubKey]int64 // Tstamp associated with a dropped root
	mutex    sync.RWMutex        // Lock for reads/writes of switchData
	data     switchData
	updater  atomic.Value    //*sync.Once
	table    atomic.Value    //lookupTable
	packetIn chan []byte     // Incoming packets for the worker to handle
	idleIn   chan switchPort // Incoming idle notifications from peer links
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
func (t *switchTable) unlockedRemovePeer(port switchPort) {
	delete(t.data.peers, port)
	t.updater.Store(&sync.Once{})
	if port != t.parent {
		return
	}
	for _, info := range t.data.peers {
		t.unlockedHandleMsg(&info.msg, info.port)
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
	t.unlockedHandleMsg(msg, fromPort)
}

// This updates the switch with information about a peer.
// Then the tricky part, it decides if it should update our own locator as a result.
// That happens if this node is already our parent, or is advertising a better root, or is advertising a better path to the same root, etc...
// There are a lot of very delicate order sensitive checks here, so its' best to just read the code if you need to understand what it's doing.
// It's very important to not change the order of the statements in the case function unless you're absolutely sure that it's safe, including safe if used along side nodes that used the previous order.
func (t *switchTable) unlockedHandleMsg(msg *switchMsg, fromPort switchPort) {
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
		sender.firstSeen = now
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
		// Special case
		// If coords changed, then this may now be a worse parent than before
		// Re-parent the node (de-parent and reprocess the message)
		// Then reprocess *all* messages to look for a better parent
		// This is so we don't keep using this node as our parent if there's something better
		t.parent = 0
		t.unlockedHandleMsg(msg, fromPort)
		for _, info := range t.data.peers {
			t.unlockedHandleMsg(&info.msg, info.port)
		}
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
// Equal to type+coords+handle for traffic packets
// Equal to type+coords+toKey+fromKey for protocol traffic packets
func switch_getPacketStreamID(packet []byte) string {
	pType, pTypeLen := wire_decode_uint64(packet)
	_, coordLen := wire_decode_coords(packet[pTypeLen:])
	end := pTypeLen + coordLen
	switch {
	case pType == wire_Traffic:
		end += handleLen // handle
	case pType == wire_ProtocolTraffic:
		end += 2 * boxPubKeyLen
	default:
		end = 0
	}
	if end > len(packet) {
		end = len(packet)
	}
	return string(packet[:end])
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
}

// Used to keep track of buffered packets
type switch_buffer struct {
	packets []switch_packetInfo // Currently buffered packets, which may be dropped if it grows too large
	count   uint64              // Total queue size, including dropped packets
}

// Clean up old packets from buffers, to help keep latency within some reasonable bound
func (t *switchTable) cleanBuffer(b *switch_buffer) {
	// TODO sane maximum buffer size, or else CoDel-like maximum time
	for len(b.packets) > 32 || (len(b.packets) > 0 && t.selfIsClosest(switch_getPacketCoords(b.packets[0].bytes))) {
		util_putBytes(b.packets[0].bytes)
		b.packets = b.packets[1:]
	}
}

// Handles incoming idle notifications
// Loops over packets and sends the newest one that's OK for this peer to send
// Returns true if the peer is no longer idle, false if it should be added to the idle list
func (t *switchTable) handleIdle(port switchPort, buffs map[string]switch_buffer) bool {
	to := t.core.peers.getPorts()[port]
	if to == nil {
		return true
	}
	var best string
	var bestSize uint64
	for streamID, buf := range buffs {
		// Filter over the streams that this node is closer to
		// Keep the one with the smallest queue
		t.cleanBuffer(&buf)
		if len(buf.packets) == 0 {
			delete(buffs, streamID)
			continue
		}
		buffs[streamID] = buf
		packet := buf.packets[0]
		coords := switch_getPacketCoords(packet.bytes)
		if (bestSize == 0 || buf.count < bestSize) && t.portIsCloser(coords, port) {
			best = streamID
			bestSize = buf.count
		}
	}
	if bestSize != 0 {
		buf := buffs[best]
		var packet switch_packetInfo
		// TODO decide if this should be LIFO or FIFO
		packet, buf.packets = buf.packets[0], buf.packets[1:]
		if len(buf.packets) == 0 {
			delete(buffs, best)
		} else {
			buffs[best] = buf
		}
		to.sendPacket(packet.bytes)
		return true
	} else {
		return false
	}
}

// The switch worker does routing lookups and sends packets to where they need to be
func (t *switchTable) doWorker() {
	buffs := make(map[string]switch_buffer) // Packets per PacketStreamID (string)
	idle := make(map[switchPort]struct{})   // this is to deduplicate things
	for {
		select {
		case packet := <-t.packetIn:
			// Try to send it somewhere (or drop it if it's corrupt or at a dead end)
			if !t.handleIn(packet, idle) {
				// There's nobody free to take it right now, so queue it for later
				// First drop random queues if we're already tracking too much, to prevent OOM DoS
				for streamID, buf := range buffs {
					if len(buffs) < 32 {
						break
					}
					for _, packet := range buf.packets {
						util_putBytes(packet.bytes)
					}
					delete(buffs, streamID)
				}
				// Now add the packet to the appropriate queue
				streamID := switch_getPacketStreamID(packet)
				buf := buffs[streamID]
				t.cleanBuffer(&buf)
				pinfo := switch_packetInfo{packet}
				buf.packets = append(buf.packets, pinfo)
				buf.count++
				buffs[streamID] = buf
			}
		case port := <-t.idleIn:
			// Try to find something to send to this peer
			if !t.handleIdle(port, buffs) {
				// Didn't find anything ready to send yet, so stay idle
				idle[port] = struct{}{}
			}
		}
	}
}
