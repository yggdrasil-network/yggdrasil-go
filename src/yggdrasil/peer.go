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
)

// The peers struct represents peers with an active connection.
// Incoming packets are passed to the corresponding peer, which handles them somehow.
// In most cases, this involves passing the packet to the handler for outgoing traffic to another peer.
// In other cases, it's link protocol traffic used to build the spanning tree, in which case this checks signatures and passes the message along to the switch.
type peers struct {
	core        *Core
	reconfigure chan chan error
	mutex       sync.Mutex   // Synchronize writes to atomic
	ports       atomic.Value //map[switchPort]*peer, use CoW semantics
}

// Initializes the peers struct.
func (ps *peers) init(c *Core) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	ps.putPorts(make(map[switchPort]*peer))
	ps.core = c
	ps.reconfigure = make(chan chan error, 1)
	go func() {
		for {
			e := <-ps.reconfigure
			e <- nil
		}
	}()
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
	bytesSent  uint64 // To track bandwidth usage for getPeers
	bytesRecvd uint64 // To track bandwidth usage for getPeers
	// BUG: sync/atomic, 32 bit platforms need the above to be the first element
	core       *Core
	intf       *linkInterface
	port       switchPort
	box        crypto.BoxPubKey
	sig        crypto.SigPubKey
	shared     crypto.BoxSharedKey
	linkShared crypto.BoxSharedKey
	endpoint   string
	firstSeen  time.Time       // To track uptime for getPeers
	linkOut    (chan []byte)   // used for protocol traffic (to bypass queues)
	doSend     (chan struct{}) // tell the linkLoop to send a switchMsg
	dinfo      (chan *dhtInfo) // used to keep the DHT working
	out        func([]byte)    // Set up by whatever created the peers struct, used to send packets to other nodes
	close      func()          // Called when a peer is removed, to close the underlying connection, or via admin api
}

// Creates a new peer with the specified box, sig, and linkShared keys, using the lowest unoccupied port number.
func (ps *peers) newPeer(box *crypto.BoxPubKey, sig *crypto.SigPubKey, linkShared *crypto.BoxSharedKey, intf *linkInterface, closer func()) *peer {
	now := time.Now()
	p := peer{box: *box,
		sig:        *sig,
		shared:     *crypto.GetSharedKey(&ps.core.boxPriv, box),
		linkShared: *linkShared,
		firstSeen:  now,
		doSend:     make(chan struct{}, 1),
		dinfo:      make(chan *dhtInfo, 1),
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
	ps.core.router.doAdmin(func() {
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
		close(p.doSend)
	}
}

// If called, sends a notification to each peer that they should send a new switch message.
// Mainly called by the switch after an update.
func (ps *peers) sendSwitchMsgs() {
	ports := ps.getPorts()
	for _, p := range ports {
		if p.port == 0 {
			continue
		}
		p.doSendSwitchMsgs()
	}
}

// If called, sends a notification to the peer's linkLoop to trigger a switchMsg send.
// Mainly called by sendSwitchMsgs or during linkLoop startup.
func (p *peer) doSendSwitchMsgs() {
	defer func() { recover() }() // In case there's a race with close(p.doSend)
	select {
	case p.doSend <- struct{}{}:
	default:
	}
}

// This must be launched in a separate goroutine by whatever sets up the peer struct.
// It handles link protocol traffic.
func (p *peer) linkLoop() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	p.doSendSwitchMsgs()
	var dinfo *dhtInfo
	for {
		select {
		case _, ok := <-p.doSend:
			if !ok {
				return
			}
			p.sendSwitchMsg()
		case dinfo = <-p.dinfo:
		case _ = <-tick.C:
			if dinfo != nil {
				p.core.dht.peers <- dinfo
			}
		}
	}
}

// Called to handle incoming packets.
// Passes the packet to a handler for that packet type.
func (p *peer) handlePacket(packet []byte) {
	// FIXME this is off by stream padding and msg length overhead, should be done in tcp.go
	atomic.AddUint64(&p.bytesRecvd, uint64(len(packet)))
	pType, pTypeLen := wire_decode_uint64(packet)
	if pTypeLen == 0 {
		return
	}
	switch pType {
	case wire_Traffic:
		p.handleTraffic(packet, pTypeLen)
	case wire_ProtocolTraffic:
		p.handleTraffic(packet, pTypeLen)
	case wire_LinkProtocolTraffic:
		p.handleLinkTraffic(packet)
	default:
		util.PutBytes(packet)
	}
	return
}

// Called to handle traffic or protocolTraffic packets.
// In either case, this reads from the coords of the packet header, does a switch lookup, and forwards to the next node.
func (p *peer) handleTraffic(packet []byte, pTypeLen int) {
	table := p.core.switchTable.getTable()
	if _, isIn := table.elems[p.port]; !isIn && p.port != 0 {
		// Drop traffic if the peer isn't in the switch
		return
	}
	p.core.switchTable.packetIn <- packet
}

// This just calls p.out(packet) for now.
func (p *peer) sendPacket(packet []byte) {
	// Is there ever a case where something more complicated is needed?
	// What if p.out blocks?
	atomic.AddUint64(&p.bytesSent, uint64(len(packet)))
	p.out(packet)
}

// This wraps the packet in the inner (ephemeral) and outer (permanent) crypto layers.
// It sends it to p.linkOut, which bypasses the usual packet queues.
func (p *peer) sendLinkPacket(packet []byte) {
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
	p.linkOut <- packet
}

// Decrypts the outer (permanent) and inner (ephemeral) crypto layers on link traffic.
// Identifies the link traffic type and calls the appropriate handler.
func (p *peer) handleLinkTraffic(bs []byte) {
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
		p.handleSwitchMsg(payload)
	default:
		util.PutBytes(bs)
	}
}

// Gets a switchMsg from the switch, adds signed next-hop info for this peer, and sends it to them.
func (p *peer) sendSwitchMsg() {
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
	p.sendLinkPacket(packet)
}

// Handles a switchMsg from the peer, checking signatures and passing good messages to the switch.
// Also creates a dhtInfo struct and arranges for it to be added to the dht (this is how dht bootstrapping begins).
func (p *peer) handleSwitchMsg(packet []byte) {
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
		p.dinfo <- nil
		return
	}
	// Pass a mesage to the dht informing it that this peer (still) exists
	loc.coords = loc.coords[:len(loc.coords)-1]
	dinfo := dhtInfo{
		key:    p.box,
		coords: loc.getCoords(),
	}
	p.dinfo <- &dinfo
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
