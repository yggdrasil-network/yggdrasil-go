// Package crypto is a wrapper around packages under golang.org/x/crypto/, particulaly curve25519, ed25519, and nacl/box.
// This is used to avoid explicitly importing and using these packages throughout yggdrasil.
// It also includes the all-important NodeID and TreeID types, which are used to identify nodes in the DHT and in the spanning tree's root selection algorithm, respectively.
package crypto

/*

This part of the package wraps crypto operations needed elsewhere

In particular, it exposes key generation for ed25519 and nacl box

It also defines NodeID and TreeID as hashes of keys, and wraps hash functions

*/

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/nacl/box"

	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

////////////////////////////////////////////////////////////////////////////////

// NodeID and TreeID

// NodeIDLen is the length (in bytes) of a NodeID.
const NodeIDLen = sha512.Size

// TreeIDLen is the length (in bytes) of a TreeID.
const TreeIDLen = sha512.Size

// handleLen is the length (in bytes) of a Handle.
const handleLen = 8

// NodeID is how a yggdrasil node is identified in the DHT, and is used to derive IPv6 addresses and subnets in the main executable. It is a sha512sum hash of the node's BoxPubKey
type NodeID [NodeIDLen]byte

// TreeID is how a yggdrasil node is identified in the root selection algorithm used to construct the spanning tree.
type TreeID [TreeIDLen]byte

type Handle [handleLen]byte

func (n *NodeID) String() string {
	return hex.EncodeToString(n[:])
}

// Network returns "nodeid" nearly always right now.
func (n *NodeID) Network() string {
	return "nodeid"
}

// PrefixLength returns the number of bits set in a masked NodeID.
func (n *NodeID) PrefixLength() int {
	var len int
	for i, v := range *n {
		_, _ = i, v
		if v == 0xff {
			len += 8
			continue
		}
		for v&0x80 != 0 {
			len++
			v <<= 1
		}
		if v != 0 {
			return -1
		}
		for i++; i < NodeIDLen; i++ {
			if n[i] != 0 {
				return -1
			}
		}
		break
	}
	return len
}

// GetNodeID returns the NodeID associated with a BoxPubKey.
func GetNodeID(pub *BoxPubKey) *NodeID {
	h := sha512.Sum512(pub[:])
	return (*NodeID)(&h)
}

// GetTreeID returns the TreeID associated with a BoxPubKey
func GetTreeID(pub *SigPubKey) *TreeID {
	h := sha512.Sum512(pub[:])
	return (*TreeID)(&h)
}

// NewHandle returns a new (cryptographically random) Handle, used by the session code to identify which session an incoming packet is associated with.
func NewHandle() *Handle {
	var h Handle
	_, err := rand.Read(h[:])
	if err != nil {
		panic(err)
	}
	return &h
}

////////////////////////////////////////////////////////////////////////////////

// Signatures

// SigPubKeyLen is the length of a SigPubKey in bytes.
const SigPubKeyLen = ed25519.PublicKeySize

// SigPrivKeyLen is the length of a SigPrivKey in bytes.
const SigPrivKeyLen = ed25519.PrivateKeySize

// SigLen is the length of SigBytes.
const SigLen = ed25519.SignatureSize

// SigPubKey is a public ed25519 signing key.
type SigPubKey [SigPubKeyLen]byte

// SigPrivKey is a private ed25519 signing key.
type SigPrivKey [SigPrivKeyLen]byte

// SigBytes is an ed25519 signature.
type SigBytes [SigLen]byte

// NewSigKeys generates a public/private ed25519 key pair.
func NewSigKeys() (*SigPubKey, *SigPrivKey) {
	var pub SigPubKey
	var priv SigPrivKey
	pubSlice, privSlice, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	copy(pub[:], pubSlice)
	copy(priv[:], privSlice)
	return &pub, &priv
}

// Sign returns the SigBytes signing a message.
func Sign(priv *SigPrivKey, msg []byte) *SigBytes {
	var sig SigBytes
	sigSlice := ed25519.Sign(priv[:], msg)
	copy(sig[:], sigSlice)
	return &sig
}

// Verify returns true if the provided signature matches the key and message.
func Verify(pub *SigPubKey, msg []byte, sig *SigBytes) bool {
	// Should sig be an array instead of a slice?...
	// It's fixed size, but
	return ed25519.Verify(pub[:], msg, sig[:])
}

// Public returns the SigPubKey associated with this SigPrivKey.
func (p SigPrivKey) Public() SigPubKey {
	priv := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	copy(priv[:], p[:])
	pub := priv.Public().(ed25519.PublicKey)
	var sigPub SigPubKey
	copy(sigPub[:], pub[:])
	return sigPub
}

////////////////////////////////////////////////////////////////////////////////

// NaCl-like crypto "box" (curve25519+xsalsa20+poly1305)

// BoxPubKeyLen is the length of a BoxPubKey in bytes.
const BoxPubKeyLen = 32

// BoxPrivKeyLen is the length of a BoxPrivKey in bytes.
const BoxPrivKeyLen = 32

// BoxSharedKeyLen is the length of a BoxSharedKey in bytes.
const BoxSharedKeyLen = 32

// BoxNonceLen is the length of a BoxNonce in bytes.
const BoxNonceLen = 24

// BoxOverhead is the length of the overhead from boxing something.
const BoxOverhead = box.Overhead

// BoxPubKey is a NaCl-like "box" public key (curve25519+xsalsa20+poly1305).
type BoxPubKey [BoxPubKeyLen]byte

// BoxPrivKey is a NaCl-like "box" private key (curve25519+xsalsa20+poly1305).
type BoxPrivKey [BoxPrivKeyLen]byte

// BoxSharedKey is a NaCl-like "box" shared key (curve25519+xsalsa20+poly1305).
type BoxSharedKey [BoxSharedKeyLen]byte

// BoxNonce is the nonce used in NaCl-like crypto "box" operations (curve25519+xsalsa20+poly1305), and must not be reused for different messages encrypted using the same BoxSharedKey.
type BoxNonce [BoxNonceLen]byte

// String returns a string representation of the "box" key.
func (k BoxPubKey) String() string {
	return hex.EncodeToString(k[:])
}

// Network returns "curve25519" for "box" keys.
func (n BoxPubKey) Network() string {
	return "curve25519"
}

// NewBoxKeys generates a new pair of public/private crypto box keys.
func NewBoxKeys() (*BoxPubKey, *BoxPrivKey) {
	pubBytes, privBytes, err := box.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	pub := (*BoxPubKey)(pubBytes)
	priv := (*BoxPrivKey)(privBytes)
	return pub, priv
}

// GetSharedKey returns the shared key derived from your private key and the destination's public key.
func GetSharedKey(myPrivKey *BoxPrivKey,
	othersPubKey *BoxPubKey) *BoxSharedKey {
	var shared [BoxSharedKeyLen]byte
	priv := (*[BoxPrivKeyLen]byte)(myPrivKey)
	pub := (*[BoxPubKeyLen]byte)(othersPubKey)
	box.Precompute(&shared, pub, priv)
	return (*BoxSharedKey)(&shared)
}

// BoxOpen returns a message and true if it successfully opens a crypto box using the provided shared key and nonce.
func BoxOpen(shared *BoxSharedKey,
	boxed []byte,
	nonce *BoxNonce) ([]byte, bool) {
	out := util.GetBytes()
	s := (*[BoxSharedKeyLen]byte)(shared)
	n := (*[BoxNonceLen]byte)(nonce)
	unboxed, success := box.OpenAfterPrecomputation(out, boxed, n, s)
	return unboxed, success
}

// BoxSeal seals a crypto box using the provided shared key, returning the box and the nonce needed to decrypt it.
// If nonce is nil, a random BoxNonce will be used and returned.
// If nonce is non-nil, then nonce.Increment() will be called before using it, and the incremented BoxNonce is what is returned.
func BoxSeal(shared *BoxSharedKey, unboxed []byte, nonce *BoxNonce) ([]byte, *BoxNonce) {
	if nonce == nil {
		nonce = NewBoxNonce()
	}
	nonce.Increment()
	out := util.GetBytes()
	s := (*[BoxSharedKeyLen]byte)(shared)
	n := (*[BoxNonceLen]byte)(nonce)
	boxed := box.SealAfterPrecomputation(out, unboxed, n, s)
	return boxed, nonce
}

// NewBoxNonce generates a (cryptographically) random BoxNonce.
func NewBoxNonce() *BoxNonce {
	var nonce BoxNonce
	_, err := rand.Read(nonce[:])
	for ; err == nil && nonce[0] == 0xff; _, err = rand.Read(nonce[:]) {
		// Make sure nonce isn't too high
		// This is just to make rollover unlikely to happen
		// Rollover is fine, but it may kill the session and force it to reopen
	}
	if err != nil {
		panic(err)
	}
	return &nonce
}

// Increment adds 2 to a BoxNonce, which is useful if one node intends to send only with odd BoxNonce values, and the other only with even BoxNonce values.
func (n *BoxNonce) Increment() {
	oldNonce := *n
	n[len(n)-1] += 2
	for i := len(n) - 2; i >= 0; i-- {
		if n[i+1] < oldNonce[i+1] {
			n[i]++
		}
	}
}

// Public returns the BoxPubKey associated with this BoxPrivKey.
func (p BoxPrivKey) Public() BoxPubKey {
	var boxPub [BoxPubKeyLen]byte
	var boxPriv [BoxPrivKeyLen]byte
	copy(boxPriv[:BoxPrivKeyLen], p[:BoxPrivKeyLen])
	curve25519.ScalarBaseMult(&boxPub, &boxPriv)
	return boxPub
}

// Minus is the result of subtracting the provided BoNonce from this BoxNonce, bounded at +- 64.
// It's primarily used to determine if a new BoxNonce is higher than the last known BoxNonce from a crypto session, and by how much.
// This is used in the machinery that makes sure replayed packets can't keep a session open indefinitely or stuck using old/bad information about a node.
func (n *BoxNonce) Minus(m *BoxNonce) int64 {
	diff := int64(0)
	for idx := range n {
		diff *= 256
		diff += int64(n[idx]) - int64(m[idx])
		if diff > 64 {
			diff = 64
		}
		if diff < -64 {
			diff = -64
		}
	}
	return diff
}
