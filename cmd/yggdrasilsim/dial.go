package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

func doListen(recvNode *simNode) {
	// TODO be able to stop the listeners somehow so they don't leak across different tests
	for {
		c, err := recvNode.listener.Accept()
		if err != nil {
			panic(err)
		}
		c.Close()
	}
}

func dialTest(sendNode, recvNode *simNode) {
	if sendNode.id == recvNode.id {
		fmt.Println("Skipping dial to self")
		return
	}
	var mask crypto.NodeID
	for idx := range mask {
		mask[idx] = 0xff
	}
	for {
		c, err := sendNode.dialer.DialByNodeIDandMask(nil, &recvNode.nodeID, &mask)
		if c != nil {
			c.Close()
			return
		}
		if err != nil {
			fmt.Println("Dial failed:", err)
		}
		time.Sleep(time.Second)
	}
}

func dialStore(store nodeStore) {
	var nodeIdxs []int
	for idx, n := range store {
		nodeIdxs = append(nodeIdxs, idx)
		go doListen(n)
	}
	sort.Slice(nodeIdxs, func(i, j int) bool {
		return nodeIdxs[i] < nodeIdxs[j]
	})
	for _, idx := range nodeIdxs {
		sendNode := store[idx]
		for _, jdx := range nodeIdxs {
			recvNode := store[jdx]
			fmt.Printf("Dialing from node %d to node %d / %d...\n", idx, jdx, len(store))
			dialTest(sendNode, recvNode)
		}
	}
}
