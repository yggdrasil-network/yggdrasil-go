package yggdrasil

// These are functions that should not exist
// They are (or were) used during development, to work around missing features
// They're also used to configure things from the outside
//  It would be better to define and export a few config functions elsewhere
//  Or define some remote API and call it to send/request configuration info

import _ "golang.org/x/net/ipv6" // TODO put this somewhere better

import "fmt"
import "net"
import "log"
import "regexp"

// Core

func (c *Core) DEBUG_getSigPub() sigPubKey {
	return (sigPubKey)(c.sigPub)
}

func (c *Core) DEBUG_getBoxPub() boxPubKey {
	return (boxPubKey)(c.boxPub)
}

func (c *Core) DEBUG_getSend() chan<- []byte {
	return c.tun.send
}

func (c *Core) DEBUG_getRecv() <-chan []byte {
	return c.tun.recv
}

// Peer

func (c *Core) DEBUG_getPeers() *peers {
	return &c.peers
}

func (ps *peers) DEBUG_newPeer(box boxPubKey,
	sig sigPubKey) *peer {
	//in <-chan []byte,
	//out chan<- []byte) *peer {
	return ps.newPeer(&box, &sig) //, in, out)
}

/*
func (ps *peers) DEBUG_startPeers() {
  ps.mutex.RLock()
  defer ps.mutex.RUnlock()
  for _, p := range ps.ports {
    if p == nil { continue }
    go p.MainLoop()
  }
}
*/

func (ps *peers) DEBUG_hasPeer(key sigPubKey) bool {
	ports := ps.ports.Load().(map[switchPort]*peer)
	for _, p := range ports {
		if p == nil {
			continue
		}
		if p.sig == key {
			return true
		}
	}
	return false
}

func (ps *peers) DEBUG_getPorts() map[switchPort]*peer {
	ports := ps.ports.Load().(map[switchPort]*peer)
	newPeers := make(map[switchPort]*peer)
	for port, p := range ports {
		newPeers[port] = p
	}
	return newPeers
}

func (p *peer) DEBUG_getSigKey() sigPubKey {
	return p.sig
}

func (p *peer) DEEBUG_getPort() switchPort {
	return p.port
}

// Router

func (c *Core) DEBUG_getSwitchTable() *switchTable {
	return &c.switchTable
}

func (c *Core) DEBUG_getLocator() switchLocator {
	return c.switchTable.getLocator()
}

func (l *switchLocator) DEBUG_getCoords() []byte {
	return l.getCoords()
}

func (c *Core) DEBUG_switchLookup(dest []byte, ttl uint64) (switchPort, uint64) {
	return c.switchTable.lookup(dest, ttl)
}

/*
func (t *switchTable) DEBUG_isDirty() bool {
  //data := t.data.Load().(*tabledata)
  t.mutex.RLock()
  defer t.mutex.RUnlock()
  data := t.data
  return data.dirty
}
*/

func (t *switchTable) DEBUG_dumpTable() {
	//data := t.data.Load().(*tabledata)
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	data := t.data
	for _, peer := range data.peers {
		//fmt.Println("DUMPTABLE:", t.treeID, peer.treeID, peer.port,
		//            peer.locator.Root, peer.coords,
		//            peer.reverse.Root, peer.reverse.Coords, peer.forward)
		fmt.Println("DUMPTABLE:", t.key, peer.key, peer.locator.coords, peer.port /*, peer.forward*/)
	}
}

func (t *switchTable) DEBUG_getReversePort(port switchPort) switchPort {
	// Returns Port(0) if it cannot get the reverse peer for any reason
	//data := t.data.Load().(*tabledata)
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	data := t.data
	if port >= switchPort(len(data.peers)) {
		return switchPort(0)
	}
	pinfo := data.peers[port]
	if len(pinfo.locator.coords) < 1 {
		return switchPort(0)
	}
	return pinfo.locator.coords[len(pinfo.locator.coords)-1]
}

// Wire

func DEBUG_wire_encode_coords(coords []byte) []byte {
	return wire_encode_coords(coords)
}

// DHT, via core

func (c *Core) DEBUG_getDHTSize() int {
	total := 0
	for bidx := 0; bidx < c.dht.nBuckets(); bidx++ {
		b := c.dht.getBucket(bidx)
		total += len(b.infos)
	}
	return total
}

// udpInterface
//  FIXME udpInterface isn't exported
//  So debug functions need to work differently...

/*
func (c *Core) DEBUG_setupLoopbackUDPInterface() {
  iface := udpInterface{}
  iface.init(c, "[::1]:0")
  c.ifaces = append(c.ifaces[:0], &iface)
}
*/

/*
func (c *Core) DEBUG_getLoopbackAddr() net.Addr {
  iface := c.ifaces[0]
  return iface.sock.LocalAddr()
}
*/

/*
func (c *Core) DEBUG_addLoopbackPeer(addr *net.UDPAddr,
                                     in (chan<- []byte),
                                     out (<-chan []byte)) {
  iface := c.ifaces[0]
  iface.addPeer(addr, in, out)
}
*/

/*
func (c *Core) DEBUG_startLoopbackUDPInterface() {
  iface := c.ifaces[0]
  go iface.reader()
  for addr, chs := range iface.peers {
    udpAddr, err := net.ResolveUDPAddr("udp6", addr)
    if err != nil { panic(err) }
    go iface.writer(udpAddr, chs.out)
  }
}
*/

////////////////////////////////////////////////////////////////////////////////

func (c *Core) DEBUG_getAddr() *address {
	return address_addrForNodeID(&c.dht.nodeID)
}

func (c *Core) DEBUG_startTun(ifname string) {
	c.DEBUG_startTunWithMTU(ifname, 1280)
}

func (c *Core) DEBUG_startTunWithMTU(ifname string, mtu int) {
	addr := c.DEBUG_getAddr()
	straddr := fmt.Sprintf("%s/%v", net.IP(addr[:]).String(), 8*len(address_prefix))
	err := c.tun.setup(ifname, straddr, mtu)
	if err != nil {
		panic(err)
	}
	go c.tun.read()
	go c.tun.write()
}

func (c *Core) DEBUG_stopTun() {
	c.tun.close()
}

////////////////////////////////////////////////////////////////////////////////

func (c *Core) DEBUG_newBoxKeys() (*boxPubKey, *boxPrivKey) {
	return newBoxKeys()
}

func (c *Core) DEBUG_newSigKeys() (*sigPubKey, *sigPrivKey) {
	return newSigKeys()
}

func (c *Core) DEBUG_getNodeID(pub *boxPubKey) *NodeID {
	return getNodeID(pub)
}

func (c *Core) DEBUG_getTreeID(pub *sigPubKey) *TreeID {
	return getTreeID(pub)
}

func (c *Core) DEBUG_addrForNodeID(nodeID *NodeID) string {
	return net.IP(address_addrForNodeID(nodeID)[:]).String()
}

func (c *Core) DEBUG_init(bpub []byte,
	bpriv []byte,
	spub []byte,
	spriv []byte) {
	var boxPub boxPubKey
	var boxPriv boxPrivKey
	var sigPub sigPubKey
	var sigPriv sigPrivKey
	copy(boxPub[:], bpub)
	copy(boxPriv[:], bpriv)
	copy(sigPub[:], spub)
	copy(sigPriv[:], spriv)
	c.init(&boxPub, &boxPriv, &sigPub, &sigPriv)
}

////////////////////////////////////////////////////////////////////////////////

func (c *Core) DEBUG_setupAndStartGlobalUDPInterface(addrport string) {
	iface := udpInterface{}
	iface.init(c, addrport)
	c.udp = &iface
}

func (c *Core) DEBUG_getGlobalUDPAddr() *net.UDPAddr {
	return c.udp.sock.LocalAddr().(*net.UDPAddr)
}

func (c *Core) DEBUG_maybeSendUDPKeys(saddr string) {
	udpAddr, err := net.ResolveUDPAddr("udp", saddr)
	if err != nil {
		panic(err)
	}
	var addr connAddr
	addr.fromUDPAddr(udpAddr)
	c.udp.mutex.RLock()
	_, isIn := c.udp.conns[addr]
	c.udp.mutex.RUnlock()
	if !isIn {
		c.udp.sendKeys(addr)
	}
}

////////////////////////////////////////////////////////////////////////////////

//*
func (c *Core) DEBUG_setupAndStartGlobalTCPInterface(addrport string) {
	iface := tcpInterface{}
	iface.init(c, addrport)
	c.tcp = &iface
}

func (c *Core) DEBUG_getGlobalTCPAddr() *net.TCPAddr {
	return c.tcp.serv.Addr().(*net.TCPAddr)
}

func (c *Core) DEBUG_addTCPConn(saddr string) {
	c.tcp.call(saddr)
}

//*/

/*
func (c *Core) DEBUG_startSelfPeer() {
  c.Peers.mutex.RLock()
  defer c.Peers.mutex.RUnlock()
  p := c.Peers.ports[0]
  go p.MainLoop()
}
*/

////////////////////////////////////////////////////////////////////////////////

/*
func (c *Core) DEBUG_setupAndStartGlobalKCPInterface(addrport string) {
  iface := kcpInterface{}
  iface.init(c, addrport)
  c.kcp = &iface
}

func (c *Core) DEBUG_getGlobalKCPAddr() net.Addr {
  return c.kcp.serv.Addr()
}

func (c *Core) DEBUG_addKCPConn(saddr string) {
  c.kcp.call(saddr)
}
*/

////////////////////////////////////////////////////////////////////////////////

func (c *Core) DEBUG_setupAndStartAdminInterface(addrport string) {
	a := admin{}
	a.init(c, addrport)
	c.admin = a
}

////////////////////////////////////////////////////////////////////////////////

func (c *Core) DEBUG_setLogger(log *log.Logger) {
	c.log = log
}

func (c *Core) DEBUG_setIfceExpr(expr *regexp.Regexp) {
	c.ifceExpr = expr
}

////////////////////////////////////////////////////////////////////////////////

func DEBUG_simLinkPeers(p, q *peer) {
	// Sets q.out() to point to p and starts p.linkLoop()
	plinkIn := make(chan []byte, 1)
	qlinkIn := make(chan []byte, 1)
	p.out = func(bs []byte) {
		go q.handlePacket(bs, qlinkIn)
	}
	q.out = func(bs []byte) {
		go p.handlePacket(bs, plinkIn)
	}
	go p.linkLoop(plinkIn)
	go q.linkLoop(qlinkIn)
}
