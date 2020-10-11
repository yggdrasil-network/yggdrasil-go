package yggdrasil

// TODO cleanup, this file is kind of a mess
//  Commented code should be removed
//  Live code should be better commented

import (
	"encoding/hex"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"

	"github.com/Arceliar/phony"
)

// The peers struct represents peers with an active connection.
// Incoming packets are passed to the corresponding peer, which handles them somehow.
// In most cases, this involves passing the packet to the handler for outgoing traffic to another peer.
// In other cases, its link protocol traffic is used to build the spanning tree, in which case this checks signatures and passes the message along to the switch.
type peers struct {
	phony.Inbox
	core  *Core
	ports map[switchPort]*peer // use CoW semantics, share updated version with each peer
	table *lookupTable         // Sent from switch, share updated version with each peer
}

// Initializes the peers struct.
func (ps *peers) init(c *Core) {
	ps.core = c
	ps.ports = make(map[switchPort]*peer)
	ps.table = new(lookupTable)
}

func (ps *peers) reconfigure() {
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

// Information known about a peer, including their box/sig keys, precomputed shared keys (static and ephemeral) and a handler for their outgoing traffic
type peer struct {
	phony.Inbox
	core       *Core
	intf       linkInterface
	port       switchPort
	box        crypto.BoxPubKey
	sig        crypto.SigPubKey
	shared     crypto.BoxSharedKey
	linkShared crypto.BoxSharedKey
	endpoint   string
	firstSeen  time.Time // To track uptime for getPeers
	dinfo      *dhtInfo  // used to keep the DHT working
	// The below aren't actually useful internally, they're just gathered for getPeers statistics
	bytesSent  uint64
	bytesRecvd uint64
	ports      map[switchPort]*peer
	table      *lookupTable
	queue      packetQueue
	max        uint64
	seq        uint64 // this and idle are used to detect when to drop packets from queue
	idle       bool
	drop       bool // set to true if we're dropping packets from the queue
}

func (ps *peers) updateTables(from phony.Actor, table *lookupTable) {
	ps.Act(from, func() {
		ps.table = table
		ps._updatePeers()
	})
}

func (ps *peers) _updatePeers() {
	ports := ps.ports
	table := ps.table
	for _, peer := range ps.ports {
		p := peer // peer is mutated during iteration
		p.Act(ps, func() {
			p.ports = ports
			p.table = table
		})
	}
}

// Creates a new peer with the specified box, sig, and linkShared keys, using the lowest unoccupied port number.
func (ps *peers) _newPeer(box *crypto.BoxPubKey, sig *crypto.SigPubKey, linkShared *crypto.BoxSharedKey, intf linkInterface) *peer {
	now := time.Now()
	p := peer{box: *box,
		core:       ps.core,
		intf:       intf,
		sig:        *sig,
		shared:     *crypto.GetSharedKey(&ps.core.boxPriv, box),
		linkShared: *linkShared,
		firstSeen:  now,
	}
	oldPorts := ps.ports
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
	ps.ports = newPorts
	ps._updatePeers()
	return &p
}

func (p *peer) _removeSelf() {
	p.core.peers.Act(p, func() {
		p.core.peers._removePeer(p)
	})
}

// Removes a peer for a given port, if one exists.
func (ps *peers) _removePeer(p *peer) {
	if q := ps.ports[p.port]; p.port == 0 || q != p {
		return
	} // Can't remove self peer or nonexistant peer
	ps.core.switchTable.forgetPeer(ps, p.port)
	oldPorts := ps.ports
	newPorts := make(map[switchPort]*peer)
	for k, v := range oldPorts {
		newPorts[k] = v
	}
	delete(newPorts, p.port)
	p.intf.close()
	ps.ports = newPorts
	ps._updatePeers()
}

// If called, sends a notification to each peer that they should send a new switch message.
// Mainly called by the switch after an update.
func (ps *peers) sendSwitchMsgs(from phony.Actor) {
	ps.Act(from, func() {
		for _, peer := range ps.ports {
			p := peer
			if p.port == 0 {
				continue
			}
			p.Act(ps, p._sendSwitchMsg)
		}
	})
}

func (ps *peers) updateDHT(from phony.Actor) {
	ps.Act(from, func() {
		for _, peer := range ps.ports {
			p := peer
			if p.port == 0 {
				continue
			}
			p.Act(ps, p._updateDHT)
		}
	})
}

// This must be launched in a separate goroutine by whatever sets up the peer struct.
func (p *peer) start() {
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
	}
}

// Get the coords of a packet without decoding
func peer_getPacketCoords(packet []byte) []byte {
	_, pTypeLen := wire_decode_uint64(packet)
	coords, _ := wire_decode_coords(packet[pTypeLen:])
	return coords
}

// Called to handle traffic or protocolTraffic packets.
// In either case, this reads from the coords of the packet header, does a switch lookup, and forwards to the next node.
func (p *peer) _handleTraffic(packet []byte) {
	if _, isIn := p.table.elems[p.port]; !isIn && p.port != 0 {
		// Drop traffic if the peer isn't in the switch
		return
	}
	coords := peer_getPacketCoords(packet)
	next := p.table.lookup(coords)
	if nPeer, isIn := p.ports[next]; isIn {
		nPeer.sendPacketFrom(p, packet)
	}
	//p.core.switchTable.packetInFrom(p, packet)
}

func (p *peer) sendPacketFrom(from phony.Actor, packet []byte) {
	p.Act(from, func() {
		p._sendPacket(packet)
	})
}

func (p *peer) _sendPacket(packet []byte) {
	p.queue.push(packet)
	if p.idle {
		p.idle = false
		p._handleIdle()
	} else if p.drop {
		for p.queue.size > p.max {
			p.queue.drop()
		}
	}
}

func (p *peer) _handleIdle() {
	var packets [][]byte
	var size uint64
	for {
		if packet, success := p.queue.pop(); success {
			packets = append(packets, packet)
			size += uint64(len(packet))
		} else {
			break
		}
	}
	p.seq++
	if len(packets) > 0 {
		p.bytesSent += uint64(size)
		p.intf.out(packets)
		p.max = p.queue.size
	} else {
		p.idle = true
	}
	p.drop = false
}

func (p *peer) notifyBlocked(from phony.Actor) {
	p.Act(from, func() {
		seq := p.seq
		p.Act(nil, func() {
			if seq == p.seq {
				p.drop = true
				p.max = 2*p.queue.size + streamMsgSize
			}
		})
	})
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
	p.intf.linkOut(packet)
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
	}
}

// Gets a switchMsg from the switch, adds signed next-hop info for this peer, and sends it to them.
func (p *peer) _sendSwitchMsg() {
	msg := p.table.getMsg()
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
		p._removeSelf()
		return
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
			p._removeSelf()
			return
		}
		prevKey = hop.Next
	}
	p.core.switchTable.Act(p, func() {
		if !p.core.switchTable._checkRoot(&msg) {
			// Bad switch message
			p.Act(&p.core.switchTable, func() {
				p.dinfo = nil
			})
		} else {
			// handle the message
			p.core.switchTable._handleMsg(&msg, p.port, false)
			p.Act(&p.core.switchTable, func() {
				// Pass a message to the dht informing it that this peer (still) exists
				loc.coords = loc.coords[:len(loc.coords)-1]
				p.dinfo = &dhtInfo{
					key:    p.box,
					coords: loc.getCoords(),
				}
				p._updateDHT()
			})
		}
	})
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
