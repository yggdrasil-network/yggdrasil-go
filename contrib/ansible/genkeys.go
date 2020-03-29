/*

This file generates crypto keys for [ansible-yggdrasil](https://github.com/jcgruenhage/ansible-yggdrasil/)

*/
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/cheggaaa/pb/v3"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

var numHosts = flag.Int("hosts", 1, "number of host vars to generate")
var keyTries = flag.Int("tries", 1000, "number of tries before taking the best keys")

type keySet struct {
	priv []byte
	pub  []byte
	id   []byte
	ip   string
}

func main() {
	flag.Parse()

	bar := pb.StartNew(*keyTries*2 + *numHosts)

	if *numHosts > *keyTries {
		println("Can't generate less keys than hosts.")
		return
	}

	var encryptionKeys []keySet
	for i := 0; i < *numHosts+1; i++ {
		encryptionKeys = append(encryptionKeys, newBoxKey())
		bar.Increment()
	}
	encryptionKeys = sortKeySetArray(encryptionKeys)
	for i := 0; i < *keyTries-*numHosts-1; i++ {
		encryptionKeys[0] = newBoxKey()
		encryptionKeys = bubbleUpTo(encryptionKeys, 0)
		bar.Increment()
	}

	var signatureKeys []keySet
	for i := 0; i < *numHosts+1; i++ {
		signatureKeys = append(signatureKeys, newSigKey())
		bar.Increment()
	}
	signatureKeys = sortKeySetArray(signatureKeys)
	for i := 0; i < *keyTries-*numHosts-1; i++ {
		signatureKeys[0] = newSigKey()
		signatureKeys = bubbleUpTo(signatureKeys, 0)
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
		file.WriteString(fmt.Sprintf("yggdrasil_encryption_public_key: %v\n", hex.EncodeToString(encryptionKeys[i].pub)))
		file.WriteString("yggdrasil_encryption_private_key: \"{{ vault_yggdrasil_encryption_private_key }}\"\n")
		file.WriteString(fmt.Sprintf("yggdrasil_signing_public_key: %v\n", hex.EncodeToString(signatureKeys[i].pub)))
		file.WriteString("yggdrasil_signing_private_key: \"{{ vault_yggdrasil_signing_private_key }}\"\n")
		file.WriteString(fmt.Sprintf("ansible_host: %v\n", encryptionKeys[i].ip))

		file, err = os.Create(fmt.Sprintf("host_vars/%x/vault", i))
		if err != nil {
			return
		}
		defer file.Close()
		file.WriteString(fmt.Sprintf("vault_yggdrasil_encryption_private_key: %v\n", hex.EncodeToString(encryptionKeys[i].priv)))
		file.WriteString(fmt.Sprintf("vault_yggdrasil_signing_private_key: %v\n", hex.EncodeToString(signatureKeys[i].priv)))
		bar.Increment()
	}
	bar.Finish()
}

func newBoxKey() keySet {
	pub, priv := crypto.NewBoxKeys()
	id := crypto.GetNodeID(pub)
	ip := net.IP(address.AddrForNodeID(id)[:]).String()
	return keySet{priv[:], pub[:], id[:], ip}
}

func newSigKey() keySet {
	pub, priv := crypto.NewSigKeys()
	id := crypto.GetTreeID(pub)
	return keySet{priv[:], pub[:], id[:], ""}
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

func sortKeySetArray(sets []keySet) []keySet {
	for i := 0; i < len(sets); i++ {
		sets = bubbleUpTo(sets, i)
	}
	return sets
}

func bubbleUpTo(sets []keySet, num int) []keySet {
	for i := 0; i < len(sets)-num-1; i++ {
		if isBetter(sets[i+1].id, sets[i].id) {
			var tmp = sets[i]
			sets[i] = sets[i+1]
			sets[i+1] = tmp
		} else {
			break
		}
	}
	return sets
}
