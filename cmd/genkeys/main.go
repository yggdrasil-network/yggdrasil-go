/*

This file generates crypto keys.
It prints out a new set of keys each time if finds a "better" one.
By default, "better" means a higher NodeID (-> higher IP address).
This is because the IP address format can compress leading 1s in the address, to increase the number of ID bits in the address.

If run with the "-sig" flag, it generates signing keys instead.
A "better" signing key means one with a higher TreeID.
This only matters if it's high enough to make you the root of the tree.

*/
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"runtime"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

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
		if isBetter(currentBest, newKey.id[:]) || len(currentBest) == 0 {
			currentBest = newKey.id
			for _, channel := range threadChannels {
				select {
				case channel <- newKey.id:
				}
			}
			fmt.Println("--------------------------------------------------------------------------------")
			switch {
			case *doSig:
				fmt.Println("sigPriv:", hex.EncodeToString(newKey.priv))
				fmt.Println("sigPub:", hex.EncodeToString(newKey.pub))
				fmt.Println("TreeID:", hex.EncodeToString(newKey.id))
			default:
				fmt.Println("boxPriv:", hex.EncodeToString(newKey.priv))
				fmt.Println("boxPub:", hex.EncodeToString(newKey.pub))
				fmt.Println("NodeID:", hex.EncodeToString(newKey.id))
				fmt.Println("IP:", newKey.ip)
			}
		}
	}
}

func isBetter(oldID, newID []byte) bool {
	for idx := range oldID {
		if newID[idx] != oldID[idx] {
			return newID[idx] > oldID[idx]
		}
	}
	return false
}

func doBoxKeys(out chan<- keySet, in <-chan []byte) {
	var bestID crypto.NodeID
	for {
		select {
		case newBestID := <-in:
			if isBetter(bestID[:], newBestID) {
				copy(bestID[:], newBestID)
			}
		default:
			pub, priv := crypto.NewBoxKeys()
			id := crypto.GetNodeID(pub)
			if !isBetter(bestID[:], id[:]) {
				continue
			}
			bestID = *id
			ip := net.IP(address.AddrForNodeID(id)[:]).String()
			out <- keySet{priv[:], pub[:], id[:], ip}
		}
	}
}

func doSigKeys(out chan<- keySet, in <-chan []byte) {
	var bestID crypto.TreeID
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
		pub, priv := crypto.NewSigKeys()
		id := crypto.GetTreeID(pub)
		if !isBetter(bestID[:], id[:]) {
			continue
		}
		bestID = *id
		out <- keySet{priv[:], pub[:], id[:], ""}
	}
}
