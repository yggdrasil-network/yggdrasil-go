package core

// This file contains the version metadata struct
// Used in the initial connection setup and key exchange
// Some of this could arguably go in wire.go instead

import "crypto/ed25519"

// This is the version-specific metadata exchanged at the start of a connection.
// It must always begin with the 4 bytes "meta" and a wire formatted uint64 major version number.
// The current version also includes a minor version number, and the box/sig/link keys that need to be exchanged to open a connection.
type version_metadata struct {
	meta [4]byte
	ver  uint8 // 1 byte in this version
	// Everything after this point potentially depends on the version number, and is subject to change in future versions
	minorVer uint8 // 1 byte in this version
	key      ed25519.PublicKey
}

// Gets a base metadata with no keys set, but with the correct version numbers.
func version_getBaseMetadata() version_metadata {
	return version_metadata{
		meta:     [4]byte{'m', 'e', 't', 'a'},
		ver:      0,
		minorVer: 4,
	}
}

// Gets the length of the metadata for this version, used to know how many bytes to read from the start of a connection.
func version_getMetaLength() (mlen int) {
	mlen += 4                     // meta
	mlen++                        // ver, as long as it's < 127, which it is in this version
	mlen++                        // minorVer, as long as it's < 127, which it is in this version
	mlen += ed25519.PublicKeySize // key
	return
}

// Encodes version metadata into its wire format.
func (m *version_metadata) encode() []byte {
	bs := make([]byte, 0, version_getMetaLength())
	bs = append(bs, m.meta[:]...)
	bs = append(bs, m.ver)
	bs = append(bs, m.minorVer)
	bs = append(bs, m.key[:]...)
	if len(bs) != version_getMetaLength() {
		panic("Inconsistent metadata length")
	}
	return bs
}

// Decodes version metadata from its wire format into the struct.
func (m *version_metadata) decode(bs []byte) bool {
	if len(bs) != version_getMetaLength() {
		return false
	}
	offset := 0
	offset += copy(m.meta[:], bs[offset:])
	m.ver, offset = bs[offset], offset+1
	m.minorVer, offset = bs[offset], offset+1
	m.key = append([]byte(nil), bs[offset:]...)
	return true
}

// Checks that the "meta" bytes and the version numbers are the expected values.
func (m *version_metadata) check() bool {
	base := version_getBaseMetadata()
	return base.meta == m.meta && base.ver == m.ver && base.minorVer == m.minorVer
}
