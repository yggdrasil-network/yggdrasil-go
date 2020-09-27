package yggdrasil

// This file contains the version metadata struct
// Used in the initial connection setup and key exchange
// Some of this could arguably go in wire.go instead

import "github.com/yggdrasil-network/yggdrasil-go/src/crypto"

// This is the version-specific metadata exchanged at the start of a connection.
// It must always begin with the 4 bytes "meta" and a wire formatted uint64 major version number.
// The current version also includes a minor version number, and the box/sig/link keys that need to be exchanged to open a connection.
type version_metadata struct {
	meta [4]byte
	ver  uint64 // 1 byte in this version
	// Everything after this point potentially depends on the version number, and is subject to change in future versions
	minorVer uint64 // 1 byte in this version
	box      crypto.BoxPubKey
	sig      crypto.SigPubKey
	link     crypto.BoxPubKey
}

// Gets a base metadata with no keys set, but with the correct version numbers.
func version_getBaseMetadata() version_metadata {
	return version_metadata{
		meta:     [4]byte{'m', 'e', 't', 'a'},
		ver:      0,
		minorVer: 2,
	}
}

// Gets the length of the metadata for this version, used to know how many bytes to read from the start of a connection.
func version_getMetaLength() (mlen int) {
	mlen += 4                   // meta
	mlen++                      // ver, as long as it's < 127, which it is in this version
	mlen++                      // minorVer, as long as it's < 127, which it is in this version
	mlen += crypto.BoxPubKeyLen // box
	mlen += crypto.SigPubKeyLen // sig
	mlen += crypto.BoxPubKeyLen // link
	return
}

// Encodes version metadata into its wire format.
func (m *version_metadata) encode() []byte {
	bs := make([]byte, 0, version_getMetaLength())
	bs = append(bs, m.meta[:]...)
	bs = append(bs, wire_encode_uint64(m.ver)...)
	bs = append(bs, wire_encode_uint64(m.minorVer)...)
	bs = append(bs, m.box[:]...)
	bs = append(bs, m.sig[:]...)
	bs = append(bs, m.link[:]...)
	if len(bs) != version_getMetaLength() {
		panic("Inconsistent metadata length")
	}
	return bs
}

// Decodes version metadata from its wire format into the struct.
func (m *version_metadata) decode(bs []byte) bool {
	switch {
	case !wire_chop_slice(m.meta[:], &bs):
		return false
	case !wire_chop_uint64(&m.ver, &bs):
		return false
	case !wire_chop_uint64(&m.minorVer, &bs):
		return false
	case !wire_chop_slice(m.box[:], &bs):
		return false
	case !wire_chop_slice(m.sig[:], &bs):
		return false
	case !wire_chop_slice(m.link[:], &bs):
		return false
	}
	return true
}

// Checks that the "meta" bytes and the version numbers are the expected values.
func (m *version_metadata) check() bool {
	base := version_getBaseMetadata()
	return base.meta == m.meta && base.ver == m.ver && base.minorVer == m.minorVer
}
