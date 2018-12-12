/*

This file generates crypto keys.
It prints out a new set of keys each time if finds a "better" one.
By default, "better" means a higher NodeID (-> higher IP address).
This is because the IP address format can compress leading 1s in the address, to incrase the number of ID bits in the address.

If run with the "-sig" flag, it generates signing keys instead.
A "better" signing key means one with a higher TreeID.
This only matters if it's high enough to make you the root of the tree.

*/
package main

import "encoding/hex"
import "flag"
import "fmt"
import "runtime"
import . "github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"

var doSig = flag.Bool("sig", false, "generate new signing keys instead")

type keySet struct {
	priv []byte
	pub  []byte
	id   []byte
	ip   string
}

func main() {
	threads := runtime.GOMAXPROCS(0)
	var threadChannels []chan []byte
	var currentBest []byte
	newKeys := make(chan keySet, threads)
	flag.Parse()

	for i := 0; i < threads; i++ {
		threadChannels = append(threadChannels, make(chan []byte, threads))
		switch {
		case *doSig:
			go doSigKeys(newKeys, threadChannels[i])
		default:
			go doBoxKeys(newKeys, threadChannels[i])
		}
	}

	for {
		newKey := <-newKeys
		if isBetter(currentBest[:], newKey.id[:]) || len(currentBest) == 0 {
			currentBest = newKey.id
			for _, channel := range threadChannels {
				select {
				case channel <- newKey.id:
				}
			}
			fmt.Println("--------------------------------------------------------------------------------")
			switch {
			case *doSig:
				fmt.Println("sigPriv:", hex.EncodeToString(newKey.priv[:]))
				fmt.Println("sigPub:", hex.EncodeToString(newKey.pub[:]))
				fmt.Println("TreeID:", hex.EncodeToString(newKey.id[:]))
			default:
				fmt.Println("boxPriv:", hex.EncodeToString(newKey.priv[:]))
				fmt.Println("boxPub:", hex.EncodeToString(newKey.pub[:]))
				fmt.Println("NodeID:", hex.EncodeToString(newKey.id[:]))
				fmt.Println("IP:", newKey.ip)
			}
		}
	}
}

func isBetter(oldID, newID []byte) bool {
	for idx := range oldID {
		if newID[idx] > oldID[idx] {
			return true
		}
		if newID[idx] < oldID[idx] {
			return false
		}
	}
	return false
}

func doBoxKeys(out chan<- keySet, in <-chan []byte) {
	c := Core{}
	pub, _ := c.DEBUG_newBoxKeys()
	bestID := c.DEBUG_getNodeID(pub)
	for idx := range bestID {
		bestID[idx] = 0
	}
	for {
		select {
		case newBestID := <-in:
			if isBetter(bestID[:], newBestID) {
				copy(bestID[:], newBestID)
			}
		default:
			pub, priv := c.DEBUG_newBoxKeys()
			id := c.DEBUG_getNodeID(pub)
			if !isBetter(bestID[:], id[:]) {
				continue
			}
			bestID = id
			ip := c.DEBUG_addrForNodeID(id)
			out <- keySet{priv[:], pub[:], id[:], ip}
		}
	}
}

func doSigKeys(out chan<- keySet, in <-chan []byte) {
	c := Core{}
	pub, _ := c.DEBUG_newSigKeys()
	bestID := c.DEBUG_getTreeID(pub)
	for idx := range bestID {
		bestID[idx] = 0
	}
	for {
		select {
		case newBestID := <-in:
			if isBetter(bestID[:], newBestID) {
				copy(bestID[:], newBestID)
			}
		default:
		}
		pub, priv := c.DEBUG_newSigKeys()
		id := c.DEBUG_getTreeID(pub)
		if !isBetter(bestID[:], id[:]) {
			continue
		}
		bestID = id
		out <- keySet{priv[:], pub[:], id[:], ""}
	}
}
