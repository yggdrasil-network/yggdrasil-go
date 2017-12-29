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
import . "yggdrasil"

var doSig = flag.Bool("sig", false, "generate new signing keys instead")

func main() {
  flag.Parse()
  switch {
    case *doSig: doSigKeys()
    default: doBoxKeys()
  }
}

func isBetter(oldID, newID []byte) bool {
  for idx := range oldID {
    if newID[idx] > oldID[idx] { return true }
    if newID[idx] < oldID[idx] { return false }
  }
  return false
}

func doBoxKeys() {
  c := Core{}
  pub, _ := c.DEBUG_newBoxKeys()
  bestID := c.DEBUG_getNodeID(pub)
  for idx := range bestID {
    bestID[idx] = 0
  }
  for {
    pub, priv := c.DEBUG_newBoxKeys()
    id := c.DEBUG_getNodeID(pub)
    if !isBetter(bestID[:], id[:]) { continue }
    bestID = id
    ip := c.DEBUG_addrForNodeID(id)
    fmt.Println("--------------------------------------------------------------------------------")
    fmt.Println("boxPriv:", hex.EncodeToString(priv[:]))
    fmt.Println("boxPub:", hex.EncodeToString(pub[:]))
    fmt.Println("NodeID:", hex.EncodeToString(id[:]))
    fmt.Println("IP:", ip)
  }
}

func doSigKeys() {
  c := Core{}
  pub, _ := c.DEBUG_newSigKeys()
  bestID := c.DEBUG_getTreeID(pub)
  for idx := range bestID {
    bestID[idx] = 0
  }
  for {
    pub, priv := c.DEBUG_newSigKeys()
    id := c.DEBUG_getTreeID(pub)
    if !isBetter(bestID[:], id[:]) { continue }
    bestID = id
    fmt.Println("--------------------------------------------------------------------------------")
    fmt.Println("sigPriv:", hex.EncodeToString(priv[:]))
    fmt.Println("sigPub:", hex.EncodeToString(pub[:]))
    fmt.Println("TreeID:", hex.EncodeToString(id[:]))
  }
}

