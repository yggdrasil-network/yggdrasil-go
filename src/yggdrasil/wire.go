package yggdrasil

// Wire formatting tools
// These are all ugly and probably not very secure

// TODO clean up unused/commented code, and add better comments to whatever is left

// Packet types, as wire_encode_uint64(type) at the start of each packet

import (
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

const (
	wire_Traffic             = iota // data being routed somewhere, handle for crypto
	wire_ProtocolTraffic            // protocol traffic, pub keys for crypto
	wire_LinkProtocolTraffic        // link proto traffic, pub keys for crypto
	wire_SwitchMsg                  // inside link protocol traffic header
	wire_SessionPing                // inside protocol traffic header
	wire_SessionPong                // inside protocol traffic header
	wire_DHTLookupRequest           // inside protocol traffic header
	wire_DHTLookupResponse          // inside protocol traffic header
	wire_NodeInfoRequest            // inside protocol traffic header
	wire_NodeInfoResponse           // inside protocol traffic header
)

// Calls wire_put_uint64 on a nil slice.
func wire_encode_uint64(elem uint64) []byte {
	return wire_put_uint64(elem, nil)
}

// Encode uint64 using a variable length scheme.
// Similar to binary.Uvarint, but big-endian.
func wire_put_uint64(e uint64, out []byte) []byte {
	var b [10]byte
	i := len(b) - 1
	b[i] = byte(e & 0x7f)
	for e >>= 7; e != 0; e >>= 7 {
		i--
		b[i] = byte(e | 0x80)
	}
	return append(out, b[i:]...)
}

// Returns the length of a wire encoded uint64 of this value.
func wire_uint64_len(elem uint64) int {
	l := 1
	for e := elem >> 7; e > 0; e >>= 7 {
		l++
	}
	return l
}

// Decode uint64 from a []byte slice.
// Returns the decoded uint64 and the number of bytes used.
func wire_decode_uint64(bs []byte) (uint64, int) {
	length := 0
	elem := uint64(0)
	for _, b := range bs {
		elem <<= 7
		elem |= uint64(b & 0x7f)
		length++
		if b&0x80 == 0 {
			break
		}
	}
	return elem, length
}

// Converts an int64 into uint64 so it can be written to the wire.
// Non-negative integers are mapped to even integers: 0 -> 0, 1 -> 2, etc.
// Negative integers are mapped to odd integers: -1 -> 1, -2 -> 3, etc.
// This means the least significant bit is a sign bit.
// This is known as zigzag encoding.
func wire_intToUint(i int64) uint64 {
	// signed arithmetic shift
	return uint64((i >> 63) ^ (i << 1))
}

// Converts uint64 back to int64, genreally when being read from the wire.
func wire_intFromUint(u uint64) int64 {
	// non-arithmetic shift
	return int64((u >> 1) ^ -(u & 1))
}

////////////////////////////////////////////////////////////////////////////////

// Takes coords, returns coords prefixed with encoded coord length.
func wire_encode_coords(coords []byte) []byte {
	coordLen := wire_encode_uint64(uint64(len(coords)))
	bs := make([]byte, 0, len(coordLen)+len(coords))
	bs = append(bs, coordLen...)
	bs = append(bs, coords...)
	return bs
}

// Puts a length prefix and the coords into bs, returns the wire formatted coords.
// Useful in hot loops where we don't want to allocate and we know the rest of the later parts of the slice are safe to overwrite.
func wire_put_coords(coords []byte, bs []byte) []byte {
	bs = wire_put_uint64(uint64(len(coords)), bs)
	bs = append(bs, coords...)
	return bs
}

// Takes a slice that begins with coords (starting with coord length).
// Returns a slice of coords and the number of bytes read.
// Used as part of various decode() functions for structs.
func wire_decode_coords(packet []byte) ([]byte, int) {
	coordLen, coordBegin := wire_decode_uint64(packet)
	coordEnd := coordBegin + int(coordLen)
	if coordBegin == 0 || coordEnd > len(packet) {
		return nil, 0
	}
	return packet[coordBegin:coordEnd], coordEnd
}

// Converts a []uint64 set of coords to a []byte set of coords.
func wire_coordsUint64stoBytes(in []uint64) (out []byte) {
	for _, coord := range in {
		c := wire_encode_uint64(coord)
		out = append(out, c...)
	}
	return out
}

// Converts a []byte set of coords to a []uint64 set of coords.
func wire_coordsBytestoUint64s(in []byte) (out []uint64) {
	offset := 0
	for {
		coord, length := wire_decode_uint64(in[offset:])
		if length == 0 {
			break
		}
		out = append(out, coord)
		offset += length
	}
	return out
}

////////////////////////////////////////////////////////////////////////////////

// Encodes a swtichMsg into its wire format.
func (m *switchMsg) encode() []byte {
	bs := wire_encode_uint64(wire_SwitchMsg)
	bs = append(bs, m.Root[:]...)
	bs = append(bs, wire_encode_uint64(wire_intToUint(m.TStamp))...)
	for _, hop := range m.Hops {
		bs = append(bs, wire_encode_uint64(uint64(hop.Port))...)
		bs = append(bs, hop.Next[:]...)
		bs = append(bs, hop.Sig[:]...)
	}
	return bs
}

// Decodes a wire formatted switchMsg into the struct, returns true if successful.
func (m *switchMsg) decode(bs []byte) bool {
	var pType uint64
	var tstamp uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_SwitchMsg:
		return false
	case !wire_chop_slice(m.Root[:], &bs):
		return false
	case !wire_chop_uint64(&tstamp, &bs):
		return false
	}
	m.TStamp = wire_intFromUint(tstamp)
	for len(bs) > 0 {
		var hop switchMsgHop
		switch {
		case !wire_chop_uint64((*uint64)(&hop.Port), &bs):
			return false
		case !wire_chop_slice(hop.Next[:], &bs):
			return false
		case !wire_chop_slice(hop.Sig[:], &bs):
			return false
		}
		m.Hops = append(m.Hops, hop)
	}
	return true
}

////////////////////////////////////////////////////////////////////////////////

// A utility function used to copy bytes into a slice and advance the beginning of the source slice, returns true if successful.
func wire_chop_slice(toSlice []byte, fromSlice *[]byte) bool {
	if len(*fromSlice) < len(toSlice) {
		return false
	}
	copy(toSlice, *fromSlice)
	*fromSlice = (*fromSlice)[len(toSlice):]
	return true
}

// A utility function to extract coords from a slice and advance the source slices, returning true if successful.
func wire_chop_coords(toCoords *[]byte, fromSlice *[]byte) bool {
	coords, coordLen := wire_decode_coords(*fromSlice)
	if coordLen == 0 {
		return false
	}
	*toCoords = append((*toCoords)[:0], coords...)
	*fromSlice = (*fromSlice)[coordLen:]
	return true
}

// A utility function to extract a wire encoded uint64 into the provided pointer while advancing the start of the source slice, returning true if successful.
func wire_chop_uint64(toUInt64 *uint64, fromSlice *[]byte) bool {
	dec, decLen := wire_decode_uint64(*fromSlice)
	if decLen == 0 {
		return false
	}
	*toUInt64 = dec
	*fromSlice = (*fromSlice)[decLen:]
	return true
}

////////////////////////////////////////////////////////////////////////////////

// Wire traffic packets

// The wire format for ordinary IPv6 traffic encapsulated by the network.
type wire_trafficPacket struct {
	Coords  []byte
	Handle  crypto.Handle
	Nonce   crypto.BoxNonce
	Payload []byte
}

// Encodes a wire_trafficPacket into its wire format.
// The returned slice was taken from the pool.
func (p *wire_trafficPacket) encode() []byte {
	bs := pool_getBytes(0)
	bs = wire_put_uint64(wire_Traffic, bs)
	bs = wire_put_coords(p.Coords, bs)
	bs = append(bs, p.Handle[:]...)
	bs = append(bs, p.Nonce[:]...)
	bs = append(bs, p.Payload...)
	return bs
}

// Decodes an encoded wire_trafficPacket into the struct, returning true if successful.
// Either way, the argument slice is added to the pool.
func (p *wire_trafficPacket) decode(bs []byte) bool {
	defer pool_putBytes(bs)
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_Traffic:
		return false
	case !wire_chop_coords(&p.Coords, &bs):
		return false
	case !wire_chop_slice(p.Handle[:], &bs):
		return false
	case !wire_chop_slice(p.Nonce[:], &bs):
		return false
	}
	p.Payload = append(p.Payload, bs...)
	return true
}

// The wire format for protocol traffic, such as dht req/res or session ping/pong packets.
type wire_protoTrafficPacket struct {
	Coords  []byte
	ToKey   crypto.BoxPubKey
	FromKey crypto.BoxPubKey
	Nonce   crypto.BoxNonce
	Payload []byte
}

// Encodes a wire_protoTrafficPacket into its wire format.
func (p *wire_protoTrafficPacket) encode() []byte {
	coords := wire_encode_coords(p.Coords)
	bs := wire_encode_uint64(wire_ProtocolTraffic)
	bs = append(bs, coords...)
	bs = append(bs, p.ToKey[:]...)
	bs = append(bs, p.FromKey[:]...)
	bs = append(bs, p.Nonce[:]...)
	bs = append(bs, p.Payload...)
	return bs
}

// Decodes an encoded wire_protoTrafficPacket into the struct, returning true if successful.
func (p *wire_protoTrafficPacket) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_ProtocolTraffic:
		return false
	case !wire_chop_coords(&p.Coords, &bs):
		return false
	case !wire_chop_slice(p.ToKey[:], &bs):
		return false
	case !wire_chop_slice(p.FromKey[:], &bs):
		return false
	case !wire_chop_slice(p.Nonce[:], &bs):
		return false
	}
	p.Payload = bs
	return true
}

// The wire format for link protocol traffic, namely switchMsg.
// There's really two layers of this, with the outer layer using permanent keys, and the inner layer using ephemeral keys.
// The keys themselves are exchanged as part of the connection setup, and then omitted from the packets.
// The two layer logic is handled in peers.go, but it's kind of ugly.
type wire_linkProtoTrafficPacket struct {
	Nonce   crypto.BoxNonce
	Payload []byte
}

// Encodes a wire_linkProtoTrafficPacket into its wire format.
func (p *wire_linkProtoTrafficPacket) encode() []byte {
	bs := wire_encode_uint64(wire_LinkProtocolTraffic)
	bs = append(bs, p.Nonce[:]...)
	bs = append(bs, p.Payload...)
	return bs
}

// Decodes an encoded wire_linkProtoTrafficPacket into the struct, returning true if successful.
func (p *wire_linkProtoTrafficPacket) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_LinkProtocolTraffic:
		return false
	case !wire_chop_slice(p.Nonce[:], &bs):
		return false
	}
	p.Payload = bs
	return true
}

////////////////////////////////////////////////////////////////////////////////

// Encodes a sessionPing into its wire format.
func (p *sessionPing) encode() []byte {
	var pTypeVal uint64
	if p.IsPong {
		pTypeVal = wire_SessionPong
	} else {
		pTypeVal = wire_SessionPing
	}
	bs := wire_encode_uint64(pTypeVal)
	//p.sendPermPub used in top level (crypto), so skipped here
	bs = append(bs, p.Handle[:]...)
	bs = append(bs, p.SendSesPub[:]...)
	bs = append(bs, wire_encode_uint64(wire_intToUint(p.Tstamp))...)
	coords := wire_encode_coords(p.Coords)
	bs = append(bs, coords...)
	bs = append(bs, wire_encode_uint64(uint64(p.MTU))...)
	return bs
}

// Decodes an encoded sessionPing into the struct, returning true if successful.
func (p *sessionPing) decode(bs []byte) bool {
	var pType uint64
	var tstamp uint64
	var mtu uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_SessionPing && pType != wire_SessionPong:
		return false
		//p.sendPermPub used in top level (crypto), so skipped here
	case !wire_chop_slice(p.Handle[:], &bs):
		return false
	case !wire_chop_slice(p.SendSesPub[:], &bs):
		return false
	case !wire_chop_uint64(&tstamp, &bs):
		return false
	case !wire_chop_coords(&p.Coords, &bs):
		return false
	case !wire_chop_uint64(&mtu, &bs):
		mtu = 1280
	}
	p.Tstamp = wire_intFromUint(tstamp)
	if pType == wire_SessionPong {
		p.IsPong = true
	}
	p.MTU = MTU(mtu)
	return true
}

////////////////////////////////////////////////////////////////////////////////

// Encodes a nodeinfoReqRes into its wire format.
func (p *nodeinfoReqRes) encode() []byte {
	var pTypeVal uint64
	if p.IsResponse {
		pTypeVal = wire_NodeInfoResponse
	} else {
		pTypeVal = wire_NodeInfoRequest
	}
	bs := wire_encode_uint64(pTypeVal)
	bs = wire_put_coords(p.SendCoords, bs)
	if pTypeVal == wire_NodeInfoResponse {
		bs = append(bs, p.NodeInfo...)
	}
	return bs
}

// Decodes an encoded nodeinfoReqRes into the struct, returning true if successful.
func (p *nodeinfoReqRes) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_NodeInfoRequest && pType != wire_NodeInfoResponse:
		return false
	case !wire_chop_coords(&p.SendCoords, &bs):
		return false
	}
	if p.IsResponse = pType == wire_NodeInfoResponse; p.IsResponse {
		if len(bs) == 0 {
			return false
		}
		p.NodeInfo = make(NodeInfoPayload, len(bs))
		if !wire_chop_slice(p.NodeInfo[:], &bs) {
			return false
		}
	}
	return true
}

////////////////////////////////////////////////////////////////////////////////

// Encodes a dhtReq into its wire format.
func (r *dhtReq) encode() []byte {
	coords := wire_encode_coords(r.Coords)
	bs := wire_encode_uint64(wire_DHTLookupRequest)
	bs = append(bs, coords...)
	bs = append(bs, r.Dest[:]...)
	return bs
}

// Decodes an encoded dhtReq into the struct, returning true if successful.
func (r *dhtReq) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_DHTLookupRequest:
		return false
	case !wire_chop_coords(&r.Coords, &bs):
		return false
	case !wire_chop_slice(r.Dest[:], &bs):
		return false
	default:
		return true
	}
}

// Encodes a dhtRes into its wire format.
func (r *dhtRes) encode() []byte {
	coords := wire_encode_coords(r.Coords)
	bs := wire_encode_uint64(wire_DHTLookupResponse)
	bs = append(bs, coords...)
	bs = append(bs, r.Dest[:]...)
	for _, info := range r.Infos {
		coords = wire_encode_coords(info.coords)
		bs = append(bs, info.key[:]...)
		bs = append(bs, coords...)
	}
	return bs
}

// Decodes an encoded dhtRes into the struct, returning true if successful.
func (r *dhtRes) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_DHTLookupResponse:
		return false
	case !wire_chop_coords(&r.Coords, &bs):
		return false
	case !wire_chop_slice(r.Dest[:], &bs):
		return false
	}
	for len(bs) > 0 {
		info := dhtInfo{}
		switch {
		case !wire_chop_slice(info.key[:], &bs):
			return false
		case !wire_chop_coords(&info.coords, &bs):
			return false
		}
		r.Infos = append(r.Infos, &info)
	}
	return true
}
