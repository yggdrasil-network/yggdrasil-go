// +build debug

package yggdrasil

// These are functions that should not exist
// They are (or were) used during development, to work around missing features
// They're also used to configure things from the outside
//  It would be better to define and export a few config functions elsewhere
//  Or define some remote API and call it to send/request configuration info

import _ "golang.org/x/net/ipv6" // TODO put this somewhere better

//import "golang.org/x/net/proxy"

import "fmt"
import "net"
import "log"
import "regexp"

import _ "net/http/pprof"
import "net/http"
import "runtime"
import "os"

import "yggdrasil/defaults"

// Start the profiler in debug builds, if the required environment variable is set.
func init() {
	envVarName := "PPROFLISTEN"
	hostPort := os.Getenv(envVarName)
	switch {
	case hostPort == "":
		fmt.Printf("DEBUG: %s not set, profiler not started.\n", envVarName)
	default:
		fmt.Printf("DEBUG: Starting pprof on %s\n", hostPort)
		go func() { fmt.Println(http.ListenAndServe(hostPort, nil)) }()
	}
}

// Starts the function profiler. This is only supported when built with
// '-tags build'.
func StartProfiler(log *log.Logger) error {
	runtime.SetBlockProfileRate(1)
	go func() { log.Println(http.ListenAndServe("localhost:6060", nil)) }()
	return nil
}

// This function is only called by the simulator to set up a node with random
// keys. It should not be used and may be removed in the future.
func (c *Core) Init() {
	bpub, bpriv := newBoxKeys()
	spub, spriv := newSigKeys()
	c.init(bpub, bpriv, spub, spriv)
	c.switchTable.start()
	c.router.start()
}

////////////////////////////////////////////////////////////////////////////////

// Core

func (c *Core) DEBUG_getSigningPublicKey() sigPubKey {
	return (sigPubKey)(c.sigPub)
}

func (c *Core) DEBUG_getEncryptionPublicKey() boxPubKey {
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

func (ps *peers) DEBUG_newPeer(box boxPubKey, sig sigPubKey, link boxSharedKey) *peer {
	//in <-chan []byte,
	//out chan<- []byte) *peer {
	return ps.newPeer(&box, &sig, &link) //, in, out)
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

func (c *Core) DEBUG_switchLookup(dest []byte) switchPort {
	return c.switchTable.DEBUG_lookup(dest)
}

// This does the switch layer lookups that decide how to route traffic.
// Traffic uses greedy routing in a metric space, where the metric distance between nodes is equal to the distance between them on the tree.
// Traffic must be routed to a node that is closer to the destination via the metric space distance.
// In the event that two nodes are equally close, it gets routed to the one with the longest uptime (due to the order that things are iterated over).
// The size of the outgoing packet queue is added to a node's tree distance when the cost of forwarding to a node, subject to the constraint that the real tree distance puts them closer to the destination than ourself.
// Doing so adds a limited form of backpressure routing, based on local information, which allows us to forward traffic around *local* bottlenecks, provided that another greedy path exists.
func (t *switchTable) DEBUG_lookup(dest []byte) switchPort {
	table := t.getTable()
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
		//p, isIn := ports[info.port]
		_, isIn := ports[info.port]
		if !isIn {
			continue
		}
		cost := int64(dist) // + p.getQueueSize()
		if cost < bestCost {
			best = info.port
			bestCost = cost
		}
	}
	return best
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
		total += len(b.peers)
		total += len(b.other)
	}
	return total
}

// TUN defaults

func (c *Core) DEBUG_GetTUNDefaultIfName() string {
	return defaults.GetDefaults().DefaultIfName
}

func (c *Core) DEBUG_GetTUNDefaultIfMTU() int {
	return defaults.GetDefaults().DefaultIfMTU
}

func (c *Core) DEBUG_GetTUNDefaultIfTAPMode() bool {
	return defaults.GetDefaults().DefaultIfTAPMode
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

func (c *Core) DEBUG_startTun(ifname string, iftapmode bool) {
	c.DEBUG_startTunWithMTU(ifname, iftapmode, 1280)
}

func (c *Core) DEBUG_startTunWithMTU(ifname string, iftapmode bool, mtu int) {
	addr := c.DEBUG_getAddr()
	straddr := fmt.Sprintf("%s/%v", net.IP(addr[:]).String(), 8*len(address_prefix))
	if ifname != "none" {
		err := c.tun.setup(ifname, iftapmode, straddr, mtu)
		if err != nil {
			panic(err)
		}
		c.log.Println("Setup TUN/TAP:", c.tun.iface.Name(), straddr)
		go func() { panic(c.tun.read()) }()
	}
	go func() { panic(c.tun.write()) }()
}

func (c *Core) DEBUG_stopTun() {
	c.tun.close()
}

////////////////////////////////////////////////////////////////////////////////

func (c *Core) DEBUG_newBoxKeys() (*boxPubKey, *boxPrivKey) {
	return newBoxKeys()
}

func (c *Core) DEBUG_getSharedKey(myPrivKey *boxPrivKey, othersPubKey *boxPubKey) *boxSharedKey {
	return getSharedKey(myPrivKey, othersPubKey)
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

	if err := c.router.start(); err != nil {
		panic(err)
	}

}

////////////////////////////////////////////////////////////////////////////////

/*
func (c *Core) DEBUG_setupAndStartGlobalUDPInterface(addrport string) {
	if err := c.udp.init(c, addrport); err != nil {
		c.log.Println("Failed to start UDP interface:", err)
		panic(err)
	}
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
*/

////////////////////////////////////////////////////////////////////////////////

func (c *Core) DEBUG_addPeer(addr string) {
	err := c.admin.addPeer(addr, "")
	if err != nil {
		panic(err)
	}
}

/*
func (c *Core) DEBUG_addSOCKSConn(socksaddr, peeraddr string) {
	go func() {
		dialer, err := proxy.SOCKS5("tcp", socksaddr, nil, proxy.Direct)
		if err == nil {
			conn, err := dialer.Dial("tcp", peeraddr)
			if err == nil {
				c.tcp.callWithConn(&wrappedConn{
					c: conn,
					raddr: &wrappedAddr{
						network: "tcp",
						addr:    peeraddr,
					},
				})
			}
		}
	}()
}
*/

//*
func (c *Core) DEBUG_setupAndStartGlobalTCPInterface(addrport string) {
	if err := c.tcp.init(c, addrport, 0, 0); err != nil {
		c.log.Println("Failed to start TCP interface:", err)
		panic(err)
	}
}

func (c *Core) DEBUG_getGlobalTCPAddr() *net.TCPAddr {
	return c.tcp.serv.Addr().(*net.TCPAddr)
}

func (c *Core) DEBUG_addTCPConn(saddr string) {
	c.tcp.call(saddr, nil, "")
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

func (c *Core) DEBUG_setupAndStartMulticastInterface() {
	m := multicast{}
	m.init(c)
	c.multicast = m
	m.start()
}

////////////////////////////////////////////////////////////////////////////////

func (c *Core) DEBUG_setLogger(log *log.Logger) {
	c.log = log
}

func (c *Core) DEBUG_setIfceExpr(expr *regexp.Regexp) {
	c.ifceExpr = append(c.ifceExpr, expr)
}

func (c *Core) DEBUG_addAllowedEncryptionPublicKey(boxStr string) {
	err := c.admin.addAllowedEncryptionPublicKey(boxStr)
	if err != nil {
		panic(err)
	}
}

////////////////////////////////////////////////////////////////////////////////

func DEBUG_simLinkPeers(p, q *peer) {
	// Sets q.out() to point to p and starts p.linkLoop()
	p.linkOut, q.linkOut = make(chan []byte, 1), make(chan []byte, 1)
	go func() {
		for bs := range p.linkOut {
			q.handlePacket(bs)
		}
	}()
	go func() {
		for bs := range q.linkOut {
			p.handlePacket(bs)
		}
	}()
	p.out = func(bs []byte) {
		p.core.switchTable.idleIn <- p.port
		go q.handlePacket(bs)
	}
	q.out = func(bs []byte) {
		q.core.switchTable.idleIn <- q.port
		go p.handlePacket(bs)
	}
	go p.linkLoop()
	go q.linkLoop()
	p.core.switchTable.idleIn <- p.port
	q.core.switchTable.idleIn <- q.port
}

func (c *Core) DEBUG_simFixMTU() {
	c.tun.mtu = 65535
}

////////////////////////////////////////////////////////////////////////////////

func Util_testAddrIDMask() {
	for idx := 0; idx < 16; idx++ {
		var orig NodeID
		orig[8] = 42
		for bidx := 0; bidx < idx; bidx++ {
			orig[bidx/8] |= (0x80 >> uint8(bidx%8))
		}
		addr := address_addrForNodeID(&orig)
		nid, mask := addr.getNodeIDandMask()
		for b := 0; b < len(mask); b++ {
			nid[b] &= mask[b]
			orig[b] &= mask[b]
		}
		if *nid != orig {
			fmt.Println(orig)
			fmt.Println(*addr)
			fmt.Println(*nid)
			fmt.Println(*mask)
			panic(idx)
		}
	}
}
