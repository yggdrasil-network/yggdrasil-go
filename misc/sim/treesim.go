// +build !lint

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/gologme/log"

	. "github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"

	. "github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

////////////////////////////////////////////////////////////////////////////////

type Node struct {
	index int
	core  Core
	send  chan<- []byte
	recv  <-chan []byte
}

func (n *Node) init(index int) {
	n.index = index
	n.core.Init()
	n.send = n.core.DEBUG_getSend()
	n.recv = n.core.DEBUG_getRecv()
	n.core.DEBUG_simFixMTU()
}

func (n *Node) printTraffic() {
	for {
		packet := <-n.recv
		fmt.Println(n.index, packet)
		//panic("Got a packet")
	}
}

func (n *Node) startPeers() {
	//for _, p := range n.core.Peers.Ports {
	//  go p.MainLoop()
	//}
	//go n.printTraffic()
	//n.core.Peers.DEBUG_startPeers()
}

func linkNodes(m, n *Node) {
	// Don't allow duplicates
	if m.core.DEBUG_getPeers().DEBUG_hasPeer(n.core.DEBUG_getSigningPublicKey()) {
		return
	}
	// Create peers
	// Buffering reduces packet loss in the sim
	//  This slightly speeds up testing (fewer delays before retrying a ping)
	pLinkPub, pLinkPriv := m.core.DEBUG_newBoxKeys()
	qLinkPub, qLinkPriv := m.core.DEBUG_newBoxKeys()
	p := m.core.DEBUG_getPeers().DEBUG_newPeer(n.core.DEBUG_getEncryptionPublicKey(),
		n.core.DEBUG_getSigningPublicKey(), *m.core.DEBUG_getSharedKey(pLinkPriv, qLinkPub))
	q := n.core.DEBUG_getPeers().DEBUG_newPeer(m.core.DEBUG_getEncryptionPublicKey(),
		m.core.DEBUG_getSigningPublicKey(), *n.core.DEBUG_getSharedKey(qLinkPriv, pLinkPub))
	DEBUG_simLinkPeers(p, q)
	return
}

func makeStoreSquareGrid(sideLength int) map[int]*Node {
	store := make(map[int]*Node)
	nNodes := sideLength * sideLength
	idxs := make([]int, 0, nNodes)
	// TODO shuffle nodeIDs
	for idx := 1; idx <= nNodes; idx++ {
		idxs = append(idxs, idx)
	}
	for _, idx := range idxs {
		node := &Node{}
		node.init(idx)
		store[idx] = node
	}
	for idx := 0; idx < nNodes; idx++ {
		if (idx % sideLength) != 0 {
			linkNodes(store[idxs[idx]], store[idxs[idx-1]])
		}
		if idx >= sideLength {
			linkNodes(store[idxs[idx]], store[idxs[idx-sideLength]])
		}
	}
	//for _, node := range store { node.initPorts() }
	return store
}

func makeStoreStar(nNodes int) map[int]*Node {
	store := make(map[int]*Node)
	center := &Node{}
	center.init(0)
	store[0] = center
	for idx := 1; idx < nNodes; idx++ {
		node := &Node{}
		node.init(idx)
		store[idx] = node
		linkNodes(center, node)
	}
	return store
}

func loadGraph(path string) map[int]*Node {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	store := make(map[int]*Node)
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		nodeIdxstrs := strings.Split(line, " ")
		nodeIdx0, _ := strconv.Atoi(nodeIdxstrs[0])
		nodeIdx1, _ := strconv.Atoi(nodeIdxstrs[1])
		if store[nodeIdx0] == nil {
			node := &Node{}
			node.init(nodeIdx0)
			store[nodeIdx0] = node
		}
		if store[nodeIdx1] == nil {
			node := &Node{}
			node.init(nodeIdx1)
			store[nodeIdx1] = node
		}
		linkNodes(store[nodeIdx0], store[nodeIdx1])
	}
	//for _, node := range store { node.initPorts() }
	return store
}

////////////////////////////////////////////////////////////////////////////////

func startNetwork(store map[[32]byte]*Node) {
	for _, node := range store {
		node.startPeers()
	}
}

func getKeyedStore(store map[int]*Node) map[[32]byte]*Node {
	newStore := make(map[[32]byte]*Node)
	for _, node := range store {
		newStore[node.core.DEBUG_getSigningPublicKey()] = node
	}
	return newStore
}

func testPaths(store map[[32]byte]*Node) bool {
	nNodes := len(store)
	count := 0
	for _, source := range store {
		count++
		fmt.Printf("Testing paths from node %d / %d (%d)\n", count, nNodes, source.index)
		for _, dest := range store {
			//if source == dest { continue }
			destLoc := dest.core.DEBUG_getLocator()
			coords := destLoc.DEBUG_getCoords()
			temp := 0
			ttl := ^uint64(0)
			oldTTL := ttl
			for here := source; here != dest; {
				temp++
				if temp > 4096 {
					fmt.Println("Loop?")
					time.Sleep(time.Second)
					return false
				}
				nextPort := here.core.DEBUG_switchLookup(coords)
				// First check if "here" is accepting packets from the previous node
				// TODO explain how this works
				ports := here.core.DEBUG_getPeers().DEBUG_getPorts()
				nextPeer := ports[nextPort]
				if nextPeer == nil {
					fmt.Println("Peer associated with next port is nil")
					return false
				}
				next := store[nextPeer.DEBUG_getSigKey()]
				/*
				   if next == here {
				     //for idx, link := range here.links {
				     //  fmt.Println("DUMP:", idx, link.nodeID)
				     //}
				     if nextPort != 0 { panic("This should not be") }
				     fmt.Println("Failed to route:", source.index, here.index, dest.index, oldTTL, ttl)
				     //here.table.DEBUG_dumpTable()
				     //fmt.Println("Ports:", here.nodeID, here.ports)
				     return false
				     panic(fmt.Sprintln("Routing Loop:",
				                        source.index,
				                        here.index,
				                        dest.index))
				   }
				*/
				if temp > 4090 {
					fmt.Println("DEBUG:",
						source.index, source.core.DEBUG_getLocator(),
						here.index, here.core.DEBUG_getLocator(),
						dest.index, dest.core.DEBUG_getLocator())
					//here.core.DEBUG_getSwitchTable().DEBUG_dumpTable()
				}
				if here != source {
					// This is sufficient to check for routing loops or blackholes
					//break
				}
				if here == next {
					fmt.Println("Drop:", source.index, here.index, dest.index, oldTTL)
					return false
				}
				here = next
			}
		}
	}
	return true
}

func stressTest(store map[[32]byte]*Node) {
	fmt.Println("Stress testing network...")
	nNodes := len(store)
	dests := make([][]byte, 0, nNodes)
	for _, dest := range store {
		loc := dest.core.DEBUG_getLocator()
		coords := loc.DEBUG_getCoords()
		dests = append(dests, coords)
	}
	lookups := 0
	start := time.Now()
	for _, source := range store {
		for _, coords := range dests {
			source.core.DEBUG_switchLookup(coords)
			lookups++
		}
	}
	timed := time.Since(start)
	fmt.Printf("%d lookups in %s (%f lookups per second)\n",
		lookups,
		timed,
		float64(lookups)/timed.Seconds())
}

func pingNodes(store map[[32]byte]*Node) {
	fmt.Println("Sending pings...")
	nNodes := len(store)
	count := 0
	equiv := func(a []byte, b []byte) bool {
		if len(a) != len(b) {
			return false
		}
		for idx := 0; idx < len(a); idx++ {
			if a[idx] != b[idx] {
				return false
			}
		}
		return true
	}
	for _, source := range store {
		count++
		//if count > 16 { break }
		fmt.Printf("Sending packets from node %d/%d (%d)\n", count, nNodes, source.index)
		sourceKey := source.core.DEBUG_getEncryptionPublicKey()
		payload := sourceKey[:]
		sourceAddr := source.core.DEBUG_getAddr()[:]
		sendTo := func(bs []byte, destAddr []byte) {
			packet := make([]byte, 40+len(bs))
			copy(packet[8:24], sourceAddr)
			copy(packet[24:40], destAddr)
			copy(packet[40:], bs)
			packet[0] = 6 << 4
			source.send <- packet
		}
		destCount := 0
		for _, dest := range store {
			destCount += 1
			fmt.Printf("%d Nodes, %d Send, %d Recv\n", nNodes, count, destCount)
			if dest == source {
				fmt.Println("Skipping self")
				continue
			}
			destAddr := dest.core.DEBUG_getAddr()[:]
			ticker := time.NewTicker(150 * time.Millisecond)
			sendTo(payload, destAddr)
			for loop := true; loop; {
				select {
				case packet := <-dest.recv:
					{
						if equiv(payload, packet[len(packet)-len(payload):]) {
							loop = false
						}
					}
				case <-ticker.C:
					sendTo(payload, destAddr)
					//dumpDHTSize(store) // note that this uses racey functions to read things...
				}
			}
			ticker.Stop()
		}
		//break // Only try sending pings from 1 node
		// This is because, for some reason, stopTun() doesn't always close it
		// And if two tuns are up, bad things happen (sends via wrong interface)
	}
	fmt.Println("Finished pinging nodes")
}

func pingBench(store map[[32]byte]*Node) {
	fmt.Println("Benchmarking pings...")
	nPings := 0
	payload := make([]byte, 1280+40) // MTU + ipv6 header
	var timed time.Duration
	//nNodes := len(store)
	count := 0
	for _, source := range store {
		count++
		//fmt.Printf("Sending packets from node %d/%d (%d)\n", count, nNodes, source.index)
		getPing := func(key [32]byte, decodedCoords []byte) []byte {
			// TODO write some function to do this the right way, put... somewhere...
			coords := DEBUG_wire_encode_coords(decodedCoords)
			packet := make([]byte, 0, len(key)+len(coords)+len(payload))
			packet = append(packet, key[:]...)
			packet = append(packet, coords...)
			packet = append(packet, payload[:]...)
			return packet
		}
		for _, dest := range store {
			key := dest.core.DEBUG_getEncryptionPublicKey()
			loc := dest.core.DEBUG_getLocator()
			coords := loc.DEBUG_getCoords()
			ping := getPing(key, coords)
			// TODO make sure the session is open first
			start := time.Now()
			for i := 0; i < 1000000; i++ {
				source.send <- ping
				nPings++
			}
			timed += time.Since(start)
			break
		}
		break
	}
	fmt.Printf("Sent %d pings in %s (%f per second)\n",
		nPings,
		timed,
		float64(nPings)/timed.Seconds())
}

func dumpStore(store map[NodeID]*Node) {
	for _, node := range store {
		fmt.Println("DUMPSTORE:", node.index, node.core.DEBUG_getLocator())
		node.core.DEBUG_getSwitchTable().DEBUG_dumpTable()
	}
}

func dumpDHTSize(store map[[32]byte]*Node) {
	var min, max, sum int
	for _, node := range store {
		num := node.core.DEBUG_getDHTSize()
		min = num
		max = num
		break
	}
	for _, node := range store {
		num := node.core.DEBUG_getDHTSize()
		if num < min {
			min = num
		}
		if num > max {
			max = num
		}
		sum += num
	}
	avg := float64(sum) / float64(len(store))
	fmt.Printf("DHT min %d / avg %f / max %d\n", min, avg, max)
}

func (n *Node) startTCP(listen string) {
	n.core.DEBUG_setupAndStartGlobalTCPInterface(listen)
}

func (n *Node) connectTCP(remoteAddr string) {
	n.core.AddPeer(remoteAddr, remoteAddr)
}

////////////////////////////////////////////////////////////////////////////////

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile `file`")
var memprofile = flag.String("memprofile", "", "write memory profile to this file")

func main() {
	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			panic(fmt.Sprintf("could not create CPU profile: ", err))
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			panic(fmt.Sprintf("could not start CPU profile: ", err))
		}
		defer pprof.StopCPUProfile()
	}
	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			panic(fmt.Sprintf("could not create memory profile: ", err))
		}
		defer func() { pprof.WriteHeapProfile(f); f.Close() }()
	}
	fmt.Println("Test")
	Util_testAddrIDMask()
	idxstore := makeStoreSquareGrid(4)
	//idxstore := makeStoreStar(256)
	//idxstore := loadGraph("misc/sim/hype-2016-09-19.list")
	//idxstore := loadGraph("misc/sim/fc00-2017-08-12.txt")
	//idxstore := loadGraph("skitter")
	kstore := getKeyedStore(idxstore)
	//*
	logger := log.New(os.Stderr, "", log.Flags())
	for _, n := range kstore {
		n.core.DEBUG_setLogger(logger)
	}
	//*/
	startNetwork(kstore)
	//time.Sleep(10*time.Second)
	// Note that testPaths only works if pressure is turned off
	//  Otherwise congestion can lead to routing loops?
	for finished := false; !finished; {
		finished = testPaths(kstore)
	}
	pingNodes(kstore)
	//pingBench(kstore) // Only after disabling debug output
	//stressTest(kstore)
	//time.Sleep(120 * time.Second)
	dumpDHTSize(kstore) // note that this uses racey functions to read things...
	if false {
		// This connects the sim to the local network
		for _, node := range kstore {
			node.startTCP("localhost:0")
			node.connectTCP("localhost:12345")
			break // just 1
		}
		for _, node := range kstore {
			go func() {
				// Just dump any packets sent to this node
				for range node.recv {
				}
			}()
		}
		var block chan struct{}
		<-block
	}
	runtime.GC()
}
