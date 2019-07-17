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
import "regexp"
import "encoding/hex"

import _ "net/http/pprof"
import "net/http"
import "runtime"
import "os"

import "github.com/gologme/log"

import "github.com/yggdrasil-network/yggdrasil-go/src/address"
import "github.com/yggdrasil-network/yggdrasil-go/src/config"
import "github.com/yggdrasil-network/yggdrasil-go/src/crypto"
import "github.com/yggdrasil-network/yggdrasil-go/src/defaults"

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
	bpub, bpriv := crypto.NewBoxKeys()
	spub, spriv := crypto.NewSigKeys()
	hbpub := hex.EncodeToString(bpub[:])
	hbpriv := hex.EncodeToString(bpriv[:])
	hspub := hex.EncodeToString(spub[:])
	hspriv := hex.EncodeToString(spriv[:])
	cfg := config.NodeConfig{
		EncryptionPublicKey:  hbpub,
		EncryptionPrivateKey: hbpriv,
		SigningPublicKey:     hspub,
		SigningPrivateKey:    hspriv,
	}
	c.config = config.NodeState{
		Current:  cfg,
		Previous: cfg,
	}
	c.init()
	c.switchTable.start()
	c.router.start()
}

////////////////////////////////////////////////////////////////////////////////

// Core

func (c *Core) DEBUG_getSigningPublicKey() crypto.SigPubKey {
	return (crypto.SigPubKey)(c.sigPub)
}

func (c *Core) DEBUG_getEncryptionPublicKey() crypto.BoxPubKey {
	return (crypto.BoxPubKey)(c.boxPub)
}

/*
func (c *Core) DEBUG_getSend() chan<- []byte {
	return c.router.tun.send
}

func (c *Core) DEBUG_getRecv() <-chan []byte {
	return c.router.tun.recv
}
*/

// Peer

func (c *Core) DEBUG_getPeers() *peers {
	return &c.peers
}

func (ps *peers) DEBUG_newPeer(box crypto.BoxPubKey, sig crypto.SigPubKey, link crypto.BoxSharedKey) *peer {
	sim := linkInterface{
		name: "(simulator)",
		info: linkInfo{
			local:    "(simulator)",
			remote:   "(simulator)",
			linkType: "sim",
		},
	}
	return ps.newPeer(&box, &sig, &link, &sim, nil)
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

func (ps *peers) DEBUG_hasPeer(key crypto.SigPubKey) bool {
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

func (p *peer) DEBUG_getSigKey() crypto.SigPubKey {
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
	var total int
	c.router.doAdmin(func() {
		total = len(c.dht.table)
	})
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

func (c *Core) DEBUG_getAddr() *address.Address {
	return address.AddrForNodeID(&c.dht.nodeID)
}

/*
func (c *Core) DEBUG_startTun(ifname string, iftapmode bool) {
	c.DEBUG_startTunWithMTU(ifname, iftapmode, 1280)
}

func (c *Core) DEBUG_startTunWithMTU(ifname string, iftapmode bool, mtu int) {
	addr := c.DEBUG_getAddr()
	straddr := fmt.Sprintf("%s/%v", net.IP(addr[:]).String(), 8*len(address.GetPrefix()))
	if ifname != "none" {
		err := c.router.tun.setup(ifname, iftapmode, straddr, mtu)
		if err != nil {
			panic(err)
		}
		c.log.Println("Setup TUN/TAP:", c.router.tun.iface.Name(), straddr)
		go func() { panic(c.router.tun.read()) }()
	}
	go func() { panic(c.router.tun.write()) }()
}

func (c *Core) DEBUG_stopTun() {
	c.router.tun.close()
}
*/

////////////////////////////////////////////////////////////////////////////////

func (c *Core) DEBUG_newBoxKeys() (*crypto.BoxPubKey, *crypto.BoxPrivKey) {
	return crypto.NewBoxKeys()
}

func (c *Core) DEBUG_getSharedKey(myPrivKey *crypto.BoxPrivKey, othersPubKey *crypto.BoxPubKey) *crypto.BoxSharedKey {
	return crypto.GetSharedKey(myPrivKey, othersPubKey)
}

func (c *Core) DEBUG_newSigKeys() (*crypto.SigPubKey, *crypto.SigPrivKey) {
	return crypto.NewSigKeys()
}

func (c *Core) DEBUG_getNodeID(pub *crypto.BoxPubKey) *crypto.NodeID {
	return crypto.GetNodeID(pub)
}

func (c *Core) DEBUG_getTreeID(pub *crypto.SigPubKey) *crypto.TreeID {
	return crypto.GetTreeID(pub)
}

func (c *Core) DEBUG_addrForNodeID(nodeID *crypto.NodeID) string {
	return net.IP(address.AddrForNodeID(nodeID)[:]).String()
}

func (c *Core) DEBUG_init(bpub []byte,
	bpriv []byte,
	spub []byte,
	spriv []byte) {
	/*var boxPub crypto.BoxPubKey
	var boxPriv crypto.BoxPrivKey
	var sigPub crypto.SigPubKey
	var sigPriv crypto.SigPrivKey
	copy(boxPub[:], bpub)
	copy(boxPriv[:], bpriv)
	copy(sigPub[:], spub)
	copy(sigPriv[:], spriv)
	c.init(&boxPub, &boxPriv, &sigPub, &sigPriv)*/
	hbpub := hex.EncodeToString(bpub[:])
	hbpriv := hex.EncodeToString(bpriv[:])
	hspub := hex.EncodeToString(spub[:])
	hspriv := hex.EncodeToString(spriv[:])
	cfg := config.NodeConfig{
		EncryptionPublicKey:  hbpub,
		EncryptionPrivateKey: hbpriv,
		SigningPublicKey:     hspub,
		SigningPrivateKey:    hspriv,
	}
	c.config = config.NodeState{
		Current:  cfg,
		Previous: cfg,
	}
	c.init()

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
/*
func (c *Core) DEBUG_addPeer(addr string) {
	err := c.admin.addPeer(addr, "")
	if err != nil {
		panic(err)
	}
}
*/
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

/*
func (c *Core) DEBUG_setupAndStartGlobalTCPInterface(addrport string) {
	c.config.Listen = []string{addrport}
	if err := c.link.init(c); err != nil {
		c.log.Println("Failed to start interfaces:", err)
		panic(err)
	}
}

func (c *Core) DEBUG_getGlobalTCPAddr() *net.TCPAddr {
	return c.link.tcp.getAddr()
}

func (c *Core) DEBUG_addTCPConn(saddr string) {
	c.link.tcp.call(saddr, nil, "")
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

/*
func (c *Core) DEBUG_setupAndStartAdminInterface(addrport string) {
	a := admin{}
	c.config.AdminListen = addrport
	a.init()
	c.admin = a
}

func (c *Core) DEBUG_setupAndStartMulticastInterface() {
	m := multicast{}
	m.init(c)
	c.multicast = m
	m.start()
}
*/

////////////////////////////////////////////////////////////////////////////////

func (c *Core) DEBUG_setLogger(log *log.Logger) {
	c.log = log
}

func (c *Core) DEBUG_setIfceExpr(expr *regexp.Regexp) {
	c.log.Println("DEBUG_setIfceExpr no longer implemented")
}

/*
func (c *Core) DEBUG_addAllowedEncryptionPublicKey(boxStr string) {
	err := c.admin.addAllowedEncryptionPublicKey(boxStr)
	if err != nil {
		panic(err)
	}
}
*/
////////////////////////////////////////////////////////////////////////////////

func DEBUG_simLinkPeers(p, q *peer) {
	// Sets q.out() to point to p and starts p.linkLoop()
	goWorkers := func(source, dest *peer) {
		source.linkOut = make(chan []byte, 1)
		send := make(chan []byte, 1)
		source.out = func(bs []byte) {
			send <- bs
		}
		go source.linkLoop()
		go func() {
			var packets [][]byte
			for {
				select {
				case packet := <-source.linkOut:
					packets = append(packets, packet)
					continue
				case packet := <-send:
					packets = append(packets, packet)
					source.core.switchTable.idleIn <- source.port
					continue
				default:
				}
				if len(packets) > 0 {
					dest.handlePacket(packets[0])
					packets = packets[1:]
					continue
				}
				select {
				case packet := <-source.linkOut:
					packets = append(packets, packet)
				case packet := <-send:
					packets = append(packets, packet)
					source.core.switchTable.idleIn <- source.port
				}
			}
		}()
	}
	goWorkers(p, q)
	goWorkers(q, p)
	p.core.switchTable.idleIn <- p.port
	q.core.switchTable.idleIn <- q.port
}

/*
func (c *Core) DEBUG_simFixMTU() {
	c.router.tun.mtu = 65535
}
*/

////////////////////////////////////////////////////////////////////////////////

func Util_testAddrIDMask() {
	for idx := 0; idx < 16; idx++ {
		var orig crypto.NodeID
		orig[8] = 42
		for bidx := 0; bidx < idx; bidx++ {
			orig[bidx/8] |= (0x80 >> uint8(bidx%8))
		}
		addr := address.AddrForNodeID(&orig)
		nid, mask := addr.GetNodeIDandMask()
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
