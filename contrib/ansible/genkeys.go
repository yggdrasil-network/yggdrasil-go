/*

This file generates crypto keys for [ansible-yggdrasil](https://github.com/jcgruenhage/ansible-yggdrasil/)

*/
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/cheggaaa/pb/v3"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

var numHosts = flag.Int("hosts", 1, "number of host vars to generate")
var keyTries = flag.Int("tries", 1000, "number of tries before taking the best keys")

type keySet struct {
	priv []byte
	pub  []byte
	ip   string
}

func main() {
	flag.Parse()

	bar := pb.StartNew(*keyTries*2 + *numHosts)

	if *numHosts > *keyTries {
		println("Can't generate less keys than hosts.")
		return
	}

	var keys []keySet
	for i := 0; i < *numHosts+1; i++ {
		keys = append(keys, newKey())
		bar.Increment()
	}
	keys = sortKeySetArray(keys)
	for i := 0; i < *keyTries-*numHosts-1; i++ {
		keys[0] = newKey()
		keys = bubbleUpTo(keys, 0)
		bar.Increment()
	}

	os.MkdirAll("host_vars", 0755)

	for i := 1; i <= *numHosts; i++ {
		os.MkdirAll(fmt.Sprintf("host_vars/%x", i), 0755)
		file, err := os.Create(fmt.Sprintf("host_vars/%x/vars", i))
		if err != nil {
			return
		}
		defer file.Close()
		file.WriteString(fmt.Sprintf("yggdrasil_public_key: %v\n", hex.EncodeToString(keys[i].pub)))
		file.WriteString("yggdrasil_private_key: \"{{ vault_yggdrasil_private_key }}\"\n")
		file.WriteString(fmt.Sprintf("ansible_host: %v\n", keys[i].ip))

		file, err = os.Create(fmt.Sprintf("host_vars/%x/vault", i))
		if err != nil {
			return
		}
		defer file.Close()
		file.WriteString(fmt.Sprintf("vault_yggdrasil_private_key: %v\n", hex.EncodeToString(keys[i].priv)))
		bar.Increment()
	}
	bar.Finish()
}

func newKey() keySet {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}
	ip := net.IP(address.AddrForKey(pub)[:]).String()
	return keySet{priv[:], pub[:], ip}
}

func isBetter(oldID, newID []byte) bool {
	for idx := range oldID {
		if newID[idx] < oldID[idx] {
			return true
		}
		if newID[idx] > oldID[idx] {
			return false
		}
	}
	return false
}

func sortKeySetArray(sets []keySet) []keySet {
	for i := 0; i < len(sets); i++ {
		sets = bubbleUpTo(sets, i)
	}
	return sets
}

func bubbleUpTo(sets []keySet, num int) []keySet {
	for i := 0; i < len(sets)-num-1; i++ {
		if isBetter(sets[i+1].pub, sets[i].pub) {
			var tmp = sets[i]
			sets[i] = sets[i+1]
			sets[i+1] = tmp
		} else {
			break
		}
	}
	return sets
}
