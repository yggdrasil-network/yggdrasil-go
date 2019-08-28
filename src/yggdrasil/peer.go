package yggdrasil

// TODO cleanup, this file is kind of a mess
//  Commented code should be removed
//  Live code should be better commented

import (
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"

	"github.com/Arceliar/phony"
)

// The peers struct represents peers with an active connection.
// Incoming packets are passed to the corresponding peer, which handles them somehow.
// In most cases, this involves passing the packet to the handler for outgoing traffic to another peer.
// In other cases, it's link protocol traffic used to build the spanning tree, in which case this checks signatures and passes the message along to the switch.
type peers struct {
	core  *Core
	mutex sync.Mutex   // Synchronize writes to atomic
	ports atomic.Value //map[switchPort]*peer, use CoW semantics
}

// Initializes the peers struct.
func (ps *peers) init(c *Core) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	ps.putPorts(make(map[switchPort]*peer))
	ps.core = c
}

func (ps *peers) reconfigure(e chan error) {
	defer close(e)
	// This is where reconfiguration would go, if we had anything to do
}

// Returns true if an incoming peer connection to a key is allowed, either
// because the key is in the whitelist or because the whitelist is empty.
func (ps *peers) isAllowedEncryptionPublicKey(box *crypto.BoxPubKey) bool {
	boxstr := hex.EncodeToString(box[:])
	ps.core.config.Mutex.RLock()
	defer ps.core.config.Mutex.RUnlock()
	for _, v := range ps.core.config.Current.AllowedEncryptionPublicKeys {
		if v == boxstr {
			return true
		}
	}
	return len(ps.core.config.Current.AllowedEncryptionPublicKeys) == 0
}

// Adds a key to the whitelist.
func (ps *peers) addAllowedEncryptionPublicKey(box string) {
	ps.core.config.Mutex.RLock()
	defer ps.core.config.Mutex.RUnlock()
	ps.core.config.Current.AllowedEncryptionPublicKeys =
		append(ps.core.config.Current.AllowedEncryptionPublicKeys, box)
}

// Removes a key from the whitelist.
func (ps *peers) removeAllowedEncryptionPublicKey(box string) {
	ps.core.config.Mutex.RLock()
	defer ps.core.config.Mutex.RUnlock()
	for k, v := range ps.core.config.Current.AllowedEncryptionPublicKeys {
		if v == box {
			ps.core.config.Current.AllowedEncryptionPublicKeys =
				append(ps.core.config.Current.AllowedEncryptionPublicKeys[:k],
					ps.core.config.Current.AllowedEncryptionPublicKeys[k+1:]...)
		}
	}
}

// Gets the whitelist of allowed keys for incoming connections.
func (ps *peers) getAllowedEncryptionPublicKeys() []string {
	ps.core.config.Mutex.RLock()
	defer ps.core.config.Mutex.RUnlock()
	return ps.core.config.Current.AllowedEncryptionPublicKeys
}

// Atomically gets a map[switchPort]*peer of known peers.
func (ps *peers) getPorts() map[switchPort]*peer {
	return ps.ports.Load().(map[switchPort]*peer)
}

// Stores a map[switchPort]*peer (note that you should take a mutex before store operations to avoid conflicts with other nodes attempting to read/change/store at the same time).
func (ps *peers) putPorts(ports map[switchPort]*peer) {
	ps.ports.Store(ports)
}

// Information known about a peer, including thier box/sig keys, precomputed shared keys (static and ephemeral) and a handler for their outgoing traffic
type peer struct {
	phony.Inbox
	core       *Core
	intf       *linkInterface
	port       switchPort
	box        crypto.BoxPubKey
	sig        crypto.SigPubKey
	shared     crypto.BoxSharedKey
	linkShared crypto.BoxSharedKey
	endpoint   string
	firstSeen  time.Time       // To track uptime for getPeers
	linkOut    func([]byte)    // used for protocol traffic (bypasses the switch)
	dinfo      *dhtInfo        // used to keep the DHT working
	out        func([][]byte)  // Set up by whatever created the peers struct, used to send packets to other nodes
	done       (chan struct{}) // closed to exit the linkLoop
	close      func()          // Called when a peer is removed, to close the underlying connection, or via admin api
	// The below aren't actually useful internally, they're just gathered for getPeers statistics
	bytesSent  uint64
	bytesRecvd uint64
}

// Creates a new peer with the specified box, sig, and linkShared keys, using the lowest unoccupied port number.
func (ps *peers) newPeer(box *crypto.BoxPubKey, sig *crypto.SigPubKey, linkShared *crypto.BoxSharedKey, intf *linkInterface, closer func()) *peer {
	now := time.Now()
	p := peer{box: *box,
		sig:        *sig,
		shared:     *crypto.GetSharedKey(&ps.core.boxPriv, box),
		linkShared: *linkShared,
		firstSeen:  now,
		done:       make(chan struct{}),
		close:      closer,
		core:       ps.core,
		intf:       intf,
	}
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	oldPorts := ps.getPorts()
	newPorts := make(map[switchPort]*peer)
	for k, v := range oldPorts {
		newPorts[k] = v
	}
	for idx := switchPort(0); true; idx++ {
		if _, isIn := newPorts[idx]; !isIn {
			p.port = switchPort(idx)
			newPorts[p.port] = &p
			break
		}
	}
	ps.putPorts(newPorts)
	return &p
}

// Removes a peer for a given port, if one exists.
func (ps *peers) removePeer(port switchPort) {
	if port == 0 {
		return
	} // Can't remove self peer
	phony.Block(&ps.core.router, func() {
		ps.core.switchTable.forgetPeer(port)
	})
	ps.mutex.Lock()
	oldPorts := ps.getPorts()
	p, isIn := oldPorts[port]
	newPorts := make(map[switchPort]*peer)
	for k, v := range oldPorts {
		newPorts[k] = v
	}
	delete(newPorts, port)
	ps.putPorts(newPorts)
	ps.mutex.Unlock()
	if isIn {
		if p.close != nil {
			p.close()
		}
		close(p.done)
	}
}

// If called, sends a notification to each peer that they should send a new switch message.
// Mainly called by the switch after an update.
func (ps *peers) sendSwitchMsgs(from phony.Actor) {
	ports := ps.getPorts()
	for _, p := range ports {
		if p.port == 0 {
			continue
		}
		p.Act(from, p._sendSwitchMsg)
	}
}

// This must be launched in a separate goroutine by whatever sets up the peer struct.
// It handles link protocol traffic.
func (p *peer) start() {
	var updateDHT func()
	updateDHT = func() {
		phony.Block(p, func() {
			select {
			case <-p.done:
			default:
				p._updateDHT()
				time.AfterFunc(time.Second, updateDHT)
			}
		})
	}
	updateDHT()
	// Just for good measure, immediately send a switch message to this peer when we start
	p.Act(nil, p._sendSwitchMsg)
}

func (p *peer) _updateDHT() {
	if p.dinfo != nil {
		p.core.router.insertPeer(p, p.dinfo)
	}
}

func (p *peer) handlePacketFrom(from phony.Actor, packet []byte) {
	p.Act(from, func() {
		p._handlePacket(packet)
	})
}

// Called to handle incoming packets.
// Passes the packet to a handler for that packet type.
func (p *peer) _handlePacket(packet []byte) {
	// FIXME this is off by stream padding and msg length overhead, should be done in tcp.go
	p.bytesRecvd += uint64(len(packet))
	pType, pTypeLen := wire_decode_uint64(packet)
	if pTypeLen == 0 {
		return
	}
	switch pType {
	case wire_Traffic:
		p._handleTraffic(packet)
	case wire_ProtocolTraffic:
		p._handleTraffic(packet)
	case wire_LinkProtocolTraffic:
		p._handleLinkTraffic(packet)
	default:
		util.PutBytes(packet)
	}
}

// Called to handle traffic or protocolTraffic packets.
// In either case, this reads from the coords of the packet header, does a switch lookup, and forwards to the next node.
func (p *peer) _handleTraffic(packet []byte) {
	table := p.core.switchTable.getTable()
	if _, isIn := table.elems[p.port]; !isIn && p.port != 0 {
		// Drop traffic if the peer isn't in the switch
		return
	}
	p.core.switchTable.packetInFrom(p, packet)
}

func (p *peer) sendPacketsFrom(from phony.Actor, packets [][]byte) {
	p.Act(from, func() {
		p._sendPackets(packets)
	})
}

// This just calls p.out(packet) for now.
func (p *peer) _sendPackets(packets [][]byte) {
	// Is there ever a case where something more complicated is needed?
	// What if p.out blocks?
	var size int
	for _, packet := range packets {
		size += len(packet)
	}
	p.bytesSent += uint64(size)
	p.out(packets)
}

// This wraps the packet in the inner (ephemeral) and outer (permanent) crypto layers.
// It sends it to p.linkOut, which bypasses the usual packet queues.
func (p *peer) _sendLinkPacket(packet []byte) {
	innerPayload, innerNonce := crypto.BoxSeal(&p.linkShared, packet, nil)
	innerLinkPacket := wire_linkProtoTrafficPacket{
		Nonce:   *innerNonce,
		Payload: innerPayload,
	}
	outerPayload := innerLinkPacket.encode()
	bs, nonce := crypto.BoxSeal(&p.shared, outerPayload, nil)
	linkPacket := wire_linkProtoTrafficPacket{
		Nonce:   *nonce,
		Payload: bs,
	}
	packet = linkPacket.encode()
	p.linkOut(packet)
}

// Decrypts the outer (permanent) and inner (ephemeral) crypto layers on link traffic.
// Identifies the link traffic type and calls the appropriate handler.
func (p *peer) _handleLinkTraffic(bs []byte) {
	packet := wire_linkProtoTrafficPacket{}
	if !packet.decode(bs) {
		return
	}
	outerPayload, isOK := crypto.BoxOpen(&p.shared, packet.Payload, &packet.Nonce)
	if !isOK {
		return
	}
	innerPacket := wire_linkProtoTrafficPacket{}
	if !innerPacket.decode(outerPayload) {
		return
	}
	payload, isOK := crypto.BoxOpen(&p.linkShared, innerPacket.Payload, &innerPacket.Nonce)
	if !isOK {
		return
	}
	pType, pTypeLen := wire_decode_uint64(payload)
	if pTypeLen == 0 {
		return
	}
	switch pType {
	case wire_SwitchMsg:
		p._handleSwitchMsg(payload)
	default:
		util.PutBytes(bs)
	}
}

// Gets a switchMsg from the switch, adds signed next-hop info for this peer, and sends it to them.
func (p *peer) _sendSwitchMsg() {
	msg := p.core.switchTable.getMsg()
	if msg == nil {
		return
	}
	bs := getBytesForSig(&p.sig, msg)
	msg.Hops = append(msg.Hops, switchMsgHop{
		Port: p.port,
		Next: p.sig,
		Sig:  *crypto.Sign(&p.core.sigPriv, bs),
	})
	packet := msg.encode()
	p._sendLinkPacket(packet)
}

// Handles a switchMsg from the peer, checking signatures and passing good messages to the switch.
// Also creates a dhtInfo struct and arranges for it to be added to the dht (this is how dht bootstrapping begins).
func (p *peer) _handleSwitchMsg(packet []byte) {
	var msg switchMsg
	if !msg.decode(packet) {
		return
	}
	if len(msg.Hops) < 1 {
		p.core.peers.removePeer(p.port)
	}
	var loc switchLocator
	prevKey := msg.Root
	for idx, hop := range msg.Hops {
		// Check signatures and collect coords for dht
		sigMsg := msg
		sigMsg.Hops = msg.Hops[:idx]
		loc.coords = append(loc.coords, hop.Port)
		bs := getBytesForSig(&hop.Next, &sigMsg)
		if !crypto.Verify(&prevKey, bs, &hop.Sig) {
			p.core.peers.removePeer(p.port)
		}
		prevKey = hop.Next
	}
	p.core.switchTable.handleMsg(&msg, p.port)
	if !p.core.switchTable.checkRoot(&msg) {
		// Bad switch message
		p.dinfo = nil
		return
	}
	// Pass a mesage to the dht informing it that this peer (still) exists
	loc.coords = loc.coords[:len(loc.coords)-1]
	p.dinfo = &dhtInfo{
		key:    p.box,
		coords: loc.getCoords(),
	}
	p._updateDHT()
}

// This generates the bytes that we sign or check the signature of for a switchMsg.
// It begins with the next node's key, followed by the root and the timestamp, followed by coords being advertised to the next node.
func getBytesForSig(next *crypto.SigPubKey, msg *switchMsg) []byte {
	var loc switchLocator
	for _, hop := range msg.Hops {
		loc.coords = append(loc.coords, hop.Port)
	}
	bs := append([]byte(nil), next[:]...)
	bs = append(bs, msg.Root[:]...)
	bs = append(bs, wire_encode_uint64(wire_intToUint(msg.TStamp))...)
	bs = append(bs, wire_encode_coords(loc.getCoords())...)
	return bs
}
