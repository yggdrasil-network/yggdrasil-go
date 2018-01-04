package main

import "fmt"
import "bufio"
import "os"
import "strings"
import "strconv"
import "time"

import "runtime/pprof"
import "flag"

import "router"

////////////////////////////////////////////////////////////////////////////////

type Node struct {
	nodeID router.NodeID
	table  router.Table
	links  []*Node
}

func (n *Node) init(nodeID router.NodeID) {
	n.nodeID = nodeID
	n.table.Init(nodeID)
	n.links = append(n.links, n)
}

func linkNodes(m, n *Node) {
	for _, o := range m.links {
		if o.nodeID == n.nodeID {
			// Don't allow duplicates
			return
		}
	}
	m.links = append(m.links, n)
	n.links = append(n.links, m)
}

func makeStoreSquareGrid(sideLength int) map[router.NodeID]*Node {
	store := make(map[router.NodeID]*Node)
	nNodes := sideLength * sideLength
	nodeIDs := make([]router.NodeID, 0, nNodes)
	// TODO shuffle nodeIDs
	for nodeID := 1; nodeID <= nNodes; nodeID++ {
		nodeIDs = append(nodeIDs, router.NodeID(nodeID))
	}
	for _, nodeID := range nodeIDs {
		node := &Node{}
		node.init(nodeID)
		store[nodeID] = node
	}
	for idx := 0; idx < nNodes; idx++ {
		if (idx % sideLength) != 0 {
			linkNodes(store[nodeIDs[idx]], store[nodeIDs[idx-1]])
		}
		if idx >= sideLength {
			linkNodes(store[nodeIDs[idx]], store[nodeIDs[idx-sideLength]])
		}
	}
	return store
}

func loadGraph(path string) map[router.NodeID]*Node {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	store := make(map[router.NodeID]*Node)
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		nodeIDstrs := strings.Split(line, " ")
		nodeIDi0, _ := strconv.Atoi(nodeIDstrs[0])
		nodeIDi1, _ := strconv.Atoi(nodeIDstrs[1])
		nodeID0 := router.NodeID(nodeIDi0)
		nodeID1 := router.NodeID(nodeIDi1)
		if store[nodeID0] == nil {
			node := &Node{}
			node.init(nodeID0)
			store[nodeID0] = node
		}
		if store[nodeID1] == nil {
			node := &Node{}
			node.init(nodeID1)
			store[nodeID1] = node
		}
		linkNodes(store[nodeID0], store[nodeID1])
	}
	return store
}

////////////////////////////////////////////////////////////////////////////////

func idleUntilConverged(store map[router.NodeID]*Node) {
	timeOfLastChange := 0
	step := 0
	// Idle untl the network has converged
	for step-timeOfLastChange < 4*router.TIMEOUT {
		step++
		fmt.Println("Step:", step, "--", "last change:", timeOfLastChange)
		for _, node := range store {
			node.table.Tick()
			for idx, link := range node.links[1:] {
				msg := node.table.CreateMessage(router.Iface(idx))
				for idx, fromNode := range link.links {
					if fromNode == node {
						//fmt.Println("Sending from node", node.nodeID, "to", link.nodeID)
						link.table.HandleMessage(msg, router.Iface(idx))
						break
					}
				}
			}
		}
		//for _, node := range store {
		//  if node.table.DEBUG_isDirty() { timeOfLastChange = step }
		//}
		//time.Sleep(10*time.Millisecond)
	}
}

func testPaths(store map[router.NodeID]*Node) {
	nNodes := len(store)
	nodeIDs := make([]router.NodeID, 0, nNodes)
	for nodeID := range store {
		nodeIDs = append(nodeIDs, nodeID)
	}
	lookups := 0
	count := 0
	start := time.Now()
	for _, source := range store {
		count++
		fmt.Printf("Testing paths from node %d / %d (%d)\n", count, nNodes, source.nodeID)
		for _, dest := range store {
			//if source == dest { continue }
			destLoc := dest.table.GetLocator()
			temp := 0
			for here := source; here != dest; {
				temp++
				if temp > 16 {
					panic("Loop?")
				}
				next := here.links[here.table.Lookup(destLoc)]
				if next == here {
					//for idx, link := range here.links {
					//  fmt.Println("DUMP:", idx, link.nodeID)
					//}
					panic(fmt.Sprintln("Routing Loop:",
						source.nodeID,
						here.nodeID,
						dest.nodeID))
				}
				//fmt.Println("DEBUG:", source.nodeID, here.nodeID, dest.nodeID)
				here = next
				lookups++
			}
		}
	}
	timed := time.Since(start)
	fmt.Printf("%f lookups per second\n", float64(lookups)/timed.Seconds())
}

func dumpStore(store map[router.NodeID]*Node) {
	for _, node := range store {
		fmt.Println("DUMPSTORE:", node.nodeID, node.table.GetLocator())
		node.table.DEBUG_dumpTable()
	}
}

////////////////////////////////////////////////////////////////////////////////

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile `file`")

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
	fmt.Println("Test")
	store := makeStoreSquareGrid(4)
	idleUntilConverged(store)
	dumpStore(store)
	testPaths(store)
	//panic("DYING")
	store = loadGraph("hype-2016-09-19.list")
	idleUntilConverged(store)
	dumpStore(store)
	testPaths(store)
}
