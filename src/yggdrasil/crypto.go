package yggdrasil

/*

This part of the package wraps crypto operations needed elsewhere

In particular, it exposes key generation for ed25519 and nacl box

It also defines NodeID and TreeID as hashes of keys, and wraps hash functions

*/

import "crypto/rand"
import "crypto/sha512"
import "golang.org/x/crypto/ed25519"
import "golang.org/x/crypto/nacl/box"

////////////////////////////////////////////////////////////////////////////////

// NodeID and TreeID

const NodeIDLen = sha512.Size
const TreeIDLen = sha512.Size
const handleLen = 8

type NodeID [NodeIDLen]byte
type TreeID [TreeIDLen]byte
type handle [handleLen]byte

func getNodeID(pub *boxPubKey) *NodeID {
  h := sha512.Sum512(pub[:])
  return (*NodeID)(&h)
}

func getTreeID(pub *sigPubKey) *TreeID {
  h := sha512.Sum512(pub[:])
  return (*TreeID)(&h)
}

func newHandle() *handle {
  var h handle
  _, err := rand.Read(h[:])
  if err != nil { panic(err) }
  return &h
}

////////////////////////////////////////////////////////////////////////////////

// Signatures

const sigPubKeyLen = ed25519.PublicKeySize
const sigPrivKeyLen = ed25519.PrivateKeySize
const sigLen = ed25519.SignatureSize

type sigPubKey [sigPubKeyLen]byte
type sigPrivKey [sigPrivKeyLen]byte
type sigBytes [sigLen]byte

func newSigKeys() (*sigPubKey, *sigPrivKey) {
  var pub sigPubKey
  var priv sigPrivKey
  pubSlice, privSlice, err := ed25519.GenerateKey(rand.Reader)
  if err != nil { panic(err) }
  copy(pub[:], pubSlice)
  copy(priv[:], privSlice)
  return &pub, &priv
}

func sign(priv *sigPrivKey, msg []byte) *sigBytes {
  var sig sigBytes
  sigSlice := ed25519.Sign(priv[:], msg)
  copy(sig[:], sigSlice)
  return &sig
}

func verify(pub *sigPubKey, msg []byte, sig *sigBytes) bool {
  // Should sig be an array instead of a slice?...
  // It's fixed size, but 
  return ed25519.Verify(pub[:], msg, sig[:])
}

////////////////////////////////////////////////////////////////////////////////

// NaCl-like crypto "box" (curve25519+xsalsa20+poly1305)

const boxPubKeyLen = 32
const boxPrivKeyLen = 32
const boxSharedKeyLen = 32
const boxNonceLen = 24

type boxPubKey [boxPubKeyLen]byte
type boxPrivKey [boxPrivKeyLen]byte
type boxSharedKey [boxSharedKeyLen]byte
type boxNonce [boxNonceLen]byte

func newBoxKeys() (*boxPubKey, *boxPrivKey) {
  pubBytes, privBytes, err := box.GenerateKey(rand.Reader)
  if err != nil { panic(err) }
  pub := (*boxPubKey)(pubBytes)
  priv := (*boxPrivKey)(privBytes)
  return pub, priv
}

func getSharedKey(myPrivKey *boxPrivKey,
                  othersPubKey *boxPubKey) *boxSharedKey {
  var shared [boxSharedKeyLen]byte
  priv := (*[boxPrivKeyLen]byte)(myPrivKey)
  pub := (*[boxPubKeyLen]byte)(othersPubKey)
  box.Precompute(&shared, pub, priv)
  return (*boxSharedKey)(&shared)
}

func boxOpen(shared *boxSharedKey,
             boxed []byte,
             nonce *boxNonce) ([]byte, bool) {
  out := util_getBytes()
  //return append(out, boxed...), true // XXX HACK to test without encryption
  s := (*[boxSharedKeyLen]byte)(shared)
  n := (*[boxNonceLen]byte)(nonce)
  unboxed, success := box.OpenAfterPrecomputation(out, boxed, n, s)
  return unboxed, success
}

func boxSeal(shared *boxSharedKey, unboxed []byte, nonce *boxNonce) ([]byte, *boxNonce) {
  if nonce == nil { nonce = newBoxNonce() }
  nonce.update()
  out := util_getBytes()
  //return append(out, unboxed...), nonce // XXX HACK to test without encryption
  s := (*[boxSharedKeyLen]byte)(shared)
  n := (*[boxNonceLen]byte)(nonce)
  boxed := box.SealAfterPrecomputation(out, unboxed, n, s)
  return boxed, nonce
}

func newBoxNonce() *boxNonce {
  var nonce boxNonce
  _, err := rand.Read(nonce[:])
  for ; err == nil && nonce[0] == 0xff ; _, err = rand.Read(nonce[:]){
    // Make sure nonce isn't too high
    // This is just to make rollover unlikely to happen
    // Rollover is fine, but it may kill the session and force it to reopen
  }
  if err != nil { panic(err) }
  return &nonce
}

func (n *boxNonce) update() {
  oldNonce := *n
  n[len(n)-1] += 2
  for i := len(n)-2 ; i >= 0 ; i-- {
    if n[i+1] < oldNonce[i+1] { n[i] += 1 }
  }
}

