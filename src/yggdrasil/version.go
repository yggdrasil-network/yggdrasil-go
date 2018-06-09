package yggdrasil

// This file contains the version metadata struct
// Used in the inital connection setup and key exchange
// Some of this could arguably go in wire.go instead

type version_metadata struct {
	meta [4]byte
	ver  uint64 // 1 byte in this version
	// Everything after this point potentially depends on the version number, and is subject to change in future versions
	minorVer uint64 // 1 byte in this version
	box      boxPubKey
	sig      sigPubKey
	link     boxPubKey
}

func version_getBaseMetadata() version_metadata {
	return version_metadata{
		meta:     [4]byte{'m', 'e', 't', 'a'},
		ver:      0,
		minorVer: 2,
	}
}

func version_getMetaLength() (mlen int) {
	mlen += 4            // meta
	mlen += 1            // ver
	mlen += 1            // minorVer
	mlen += boxPubKeyLen // box
	mlen += sigPubKeyLen // sig
	mlen += boxPubKeyLen // link
	return
}

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

func (m *version_metadata) check() bool {
	base := version_getBaseMetadata()
	return base.meta == m.meta && base.ver == m.ver && base.minorVer == m.minorVer
}
