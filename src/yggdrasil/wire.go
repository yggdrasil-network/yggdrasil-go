package yggdrasil

// Wire formatting tools
// These are all ugly and probably not very secure

// TODO clean up unused/commented code, and add better comments to whatever is left

// Packet types, as an Encode_uint64 at the start of each packet
// TODO? make things still work after reordering (after things stabilize more?)
//  Type safety would also be nice, `type wire_type uint64`, rewrite as needed?
const (
	wire_Traffic             = iota // data being routed somewhere, handle for crypto
	wire_ProtocolTraffic            // protocol traffic, pub keys for crypto
	wire_LinkProtocolTraffic        // link proto traffic, pub keys for crypto
	wire_SwitchAnnounce             // inside protocol traffic header
	wire_SwitchHopRequest           // inside protocol traffic header
	wire_SwitchHop                  // inside protocol traffic header
	wire_SessionPing                // inside protocol traffic header
	wire_SessionPong                // inside protocol traffic header
	wire_DHTLookupRequest           // inside protocol traffic header
	wire_DHTLookupResponse          // inside protocol traffic header
	wire_SearchRequest              // inside protocol traffic header
	wire_SearchResponse             // inside protocol traffic header
	//wire_Keys                 // udp key packet (boxPub, sigPub)
)

// Encode uint64 using a variable length scheme
// Similar to binary.Uvarint, but big-endian
func wire_encode_uint64(elem uint64) []byte {
	return wire_put_uint64(elem, nil)
}

// Occasionally useful for appending to an existing slice (if there's room)
func wire_put_uint64(elem uint64, out []byte) []byte {
	bs := make([]byte, 0, 10)
	bs = append(bs, byte(elem&0x7f))
	for e := elem >> 7; e > 0; e >>= 7 {
		bs = append(bs, byte(e|0x80))
	}
	// Now reverse bytes, because we set them in the wrong order
	// TODO just put them in the right place the first time...
	last := len(bs) - 1
	for idx := 0; idx < len(bs)/2; idx++ {
		bs[idx], bs[last-idx] = bs[last-idx], bs[idx]
	}
	return append(out, bs...)
}

func wire_uint64_len(elem uint64) int {
	l := 1
	for e := elem >> 7; e > 0; e >>= 7 {
		l++
	}
	return l
}

// Decode uint64 from a []byte slice
// Returns the decoded uint64 and the number of bytes used
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

func wire_intToUint(i int64) uint64 {
	var u uint64
	if i < 0 {
		u = uint64(-i) << 1
		u |= 0x01 // sign bit
	} else {
		u = uint64(i) << 1
	}
	return u
}

func wire_intFromUint(u uint64) int64 {
	var i int64
	i = int64(u >> 1)
	if u&0x01 != 0 {
		i *= -1
	}
	return i
}

////////////////////////////////////////////////////////////////////////////////

// Takes coords, returns coords prefixed with encoded coord length
func wire_encode_coords(coords []byte) []byte {
	coordLen := wire_encode_uint64(uint64(len(coords)))
	bs := make([]byte, 0, len(coordLen)+len(coords))
	bs = append(bs, coordLen...)
	bs = append(bs, coords...)
	return bs
}

func wire_put_coords(coords []byte, bs []byte) []byte {
	bs = wire_put_uint64(uint64(len(coords)), bs)
	bs = append(bs, coords...)
	return bs
}

// Takes a packet that begins with coords (starting with coord length)
// Returns a slice of coords and the number of bytes read
func wire_decode_coords(packet []byte) ([]byte, int) {
	coordLen, coordBegin := wire_decode_uint64(packet)
	coordEnd := coordBegin + int(coordLen)
	//if coordBegin == 0 { panic("No coords found") } // Testing
	//if coordEnd > len(packet) { panic("Packet too short") } // Testing
	if coordBegin == 0 || coordEnd > len(packet) {
		return nil, 0
	}
	return packet[coordBegin:coordEnd], coordEnd
}

////////////////////////////////////////////////////////////////////////////////

// Announces that we can send parts of a Message with a particular seq
type msgAnnounce struct {
	root   sigPubKey
	tstamp int64
	seq    uint64
	len    uint64
	//Deg uint64
	//RSeq uint64
}

func (m *msgAnnounce) encode() []byte {
	bs := wire_encode_uint64(wire_SwitchAnnounce)
	bs = append(bs, m.root[:]...)
	bs = append(bs, wire_encode_uint64(wire_intToUint(m.tstamp))...)
	bs = append(bs, wire_encode_uint64(m.seq)...)
	bs = append(bs, wire_encode_uint64(m.len)...)
	//bs = append(bs, wire_encode_uint64(m.Deg)...)
	//bs = append(bs, wire_encode_uint64(m.RSeq)...)
	return bs
}

func (m *msgAnnounce) decode(bs []byte) bool {
	var pType uint64
	var tstamp uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_SwitchAnnounce:
		return false
	case !wire_chop_slice(m.root[:], &bs):
		return false
	case !wire_chop_uint64(&tstamp, &bs):
		return false
	case !wire_chop_uint64(&m.seq, &bs):
		return false
	case !wire_chop_uint64(&m.len, &bs):
		return false
		//case !wire_chop_uint64(&m.Deg, &bs): return false
		//case !wire_chop_uint64(&m.RSeq, &bs): return false
	}
	m.tstamp = wire_intFromUint(tstamp)
	return true
}

type msgHopReq struct {
	root   sigPubKey
	tstamp int64
	seq    uint64
	hop    uint64
}

func (m *msgHopReq) encode() []byte {
	bs := wire_encode_uint64(wire_SwitchHopRequest)
	bs = append(bs, m.root[:]...)
	bs = append(bs, wire_encode_uint64(wire_intToUint(m.tstamp))...)
	bs = append(bs, wire_encode_uint64(m.seq)...)
	bs = append(bs, wire_encode_uint64(m.hop)...)
	return bs
}

func (m *msgHopReq) decode(bs []byte) bool {
	var pType uint64
	var tstamp uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_SwitchHopRequest:
		return false
	case !wire_chop_slice(m.root[:], &bs):
		return false
	case !wire_chop_uint64(&tstamp, &bs):
		return false
	case !wire_chop_uint64(&m.seq, &bs):
		return false
	case !wire_chop_uint64(&m.hop, &bs):
		return false
	}
	m.tstamp = wire_intFromUint(tstamp)
	return true
}

type msgHop struct {
	root   sigPubKey
	tstamp int64
	seq    uint64
	hop    uint64
	port   switchPort
	next   sigPubKey
	sig    sigBytes
}

func (m *msgHop) encode() []byte {
	bs := wire_encode_uint64(wire_SwitchHop)
	bs = append(bs, m.root[:]...)
	bs = append(bs, wire_encode_uint64(wire_intToUint(m.tstamp))...)
	bs = append(bs, wire_encode_uint64(m.seq)...)
	bs = append(bs, wire_encode_uint64(m.hop)...)
	bs = append(bs, wire_encode_uint64(uint64(m.port))...)
	bs = append(bs, m.next[:]...)
	bs = append(bs, m.sig[:]...)
	return bs
}

func (m *msgHop) decode(bs []byte) bool {
	var pType uint64
	var tstamp uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_SwitchHop:
		return false
	case !wire_chop_slice(m.root[:], &bs):
		return false
	case !wire_chop_uint64(&tstamp, &bs):
		return false
	case !wire_chop_uint64(&m.seq, &bs):
		return false
	case !wire_chop_uint64(&m.hop, &bs):
		return false
	case !wire_chop_uint64((*uint64)(&m.port), &bs):
		return false
	case !wire_chop_slice(m.next[:], &bs):
		return false
	case !wire_chop_slice(m.sig[:], &bs):
		return false
	}
	m.tstamp = wire_intFromUint(tstamp)
	return true
}

// Format used to check signatures only, so no need to also support decoding
func wire_encode_locator(loc *switchLocator) []byte {
	coords := wire_encode_coords(loc.getCoords())
	var bs []byte
	bs = append(bs, loc.root[:]...)
	bs = append(bs, wire_encode_uint64(wire_intToUint(loc.tstamp))...)
	bs = append(bs, coords...)
	return bs
}

func wire_chop_slice(toSlice []byte, fromSlice *[]byte) bool {
	if len(*fromSlice) < len(toSlice) {
		return false
	}
	copy(toSlice, *fromSlice)
	*fromSlice = (*fromSlice)[len(toSlice):]
	return true
}

func wire_chop_coords(toCoords *[]byte, fromSlice *[]byte) bool {
	coords, coordLen := wire_decode_coords(*fromSlice)
	if coordLen == 0 {
		return false
	}
	*toCoords = append((*toCoords)[:0], coords...)
	*fromSlice = (*fromSlice)[coordLen:]
	return true
}

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

type wire_trafficPacket struct {
	ttl     uint64
	coords  []byte
	handle  handle
	nonce   boxNonce
	payload []byte
}

// This is basically MarshalBinary, but decode doesn't allow that...
func (p *wire_trafficPacket) encode() []byte {
	bs := util_getBytes()
	bs = wire_put_uint64(wire_Traffic, bs)
	bs = wire_put_uint64(p.ttl, bs)
	bs = wire_put_coords(p.coords, bs)
	bs = append(bs, p.handle[:]...)
	bs = append(bs, p.nonce[:]...)
	bs = append(bs, p.payload...)
	return bs
}

// Not just UnmarshalBinary becuase the original slice isn't always copied from
func (p *wire_trafficPacket) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_Traffic:
		return false
	case !wire_chop_uint64(&p.ttl, &bs):
		return false
	case !wire_chop_coords(&p.coords, &bs):
		return false
	case !wire_chop_slice(p.handle[:], &bs):
		return false
	case !wire_chop_slice(p.nonce[:], &bs):
		return false
	}
	p.payload = append(util_getBytes(), bs...)
	return true
}

type wire_protoTrafficPacket struct {
	ttl     uint64
	coords  []byte
	toKey   boxPubKey
	fromKey boxPubKey
	nonce   boxNonce
	payload []byte
}

func (p *wire_protoTrafficPacket) encode() []byte {
	coords := wire_encode_coords(p.coords)
	bs := wire_encode_uint64(wire_ProtocolTraffic)
	bs = append(bs, wire_encode_uint64(p.ttl)...)
	bs = append(bs, coords...)
	bs = append(bs, p.toKey[:]...)
	bs = append(bs, p.fromKey[:]...)
	bs = append(bs, p.nonce[:]...)
	bs = append(bs, p.payload...)
	return bs
}

func (p *wire_protoTrafficPacket) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_ProtocolTraffic:
		return false
	case !wire_chop_uint64(&p.ttl, &bs):
		return false
	case !wire_chop_coords(&p.coords, &bs):
		return false
	case !wire_chop_slice(p.toKey[:], &bs):
		return false
	case !wire_chop_slice(p.fromKey[:], &bs):
		return false
	case !wire_chop_slice(p.nonce[:], &bs):
		return false
	}
	p.payload = bs
	return true
}

type wire_linkProtoTrafficPacket struct {
	toKey   boxPubKey
	fromKey boxPubKey
	nonce   boxNonce
	payload []byte
}

func (p *wire_linkProtoTrafficPacket) encode() []byte {
	bs := wire_encode_uint64(wire_LinkProtocolTraffic)
	bs = append(bs, p.toKey[:]...)
	bs = append(bs, p.fromKey[:]...)
	bs = append(bs, p.nonce[:]...)
	bs = append(bs, p.payload...)
	return bs
}

func (p *wire_linkProtoTrafficPacket) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_LinkProtocolTraffic:
		return false
	case !wire_chop_slice(p.toKey[:], &bs):
		return false
	case !wire_chop_slice(p.fromKey[:], &bs):
		return false
	case !wire_chop_slice(p.nonce[:], &bs):
		return false
	}
	p.payload = bs
	return true
}

////////////////////////////////////////////////////////////////////////////////

func (p *sessionPing) encode() []byte {
	var pTypeVal uint64
	if p.isPong {
		pTypeVal = wire_SessionPong
	} else {
		pTypeVal = wire_SessionPing
	}
	bs := wire_encode_uint64(pTypeVal)
	//p.sendPermPub used in top level (crypto), so skipped here
	bs = append(bs, p.handle[:]...)
	bs = append(bs, p.sendSesPub[:]...)
	bs = append(bs, wire_encode_uint64(wire_intToUint(p.tstamp))...)
	coords := wire_encode_coords(p.coords)
	bs = append(bs, coords...)
	bs = append(bs, wire_encode_uint64(uint64(p.mtu))...)
	return bs
}

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
	case !wire_chop_slice(p.handle[:], &bs):
		return false
	case !wire_chop_slice(p.sendSesPub[:], &bs):
		return false
	case !wire_chop_uint64(&tstamp, &bs):
		return false
	case !wire_chop_coords(&p.coords, &bs):
		return false
	case !wire_chop_uint64(&mtu, &bs):
		mtu = 1280
	}
	p.tstamp = wire_intFromUint(tstamp)
	if pType == wire_SessionPong {
		p.isPong = true
	}
	p.mtu = uint16(mtu)
	return true
}

////////////////////////////////////////////////////////////////////////////////

func (r *dhtReq) encode() []byte {
	coords := wire_encode_coords(r.coords)
	bs := wire_encode_uint64(wire_DHTLookupRequest)
	bs = append(bs, r.key[:]...)
	bs = append(bs, coords...)
	bs = append(bs, r.dest[:]...)
	return bs
}

func (r *dhtReq) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_DHTLookupRequest:
		return false
	case !wire_chop_slice(r.key[:], &bs):
		return false
	case !wire_chop_coords(&r.coords, &bs):
		return false
	case !wire_chop_slice(r.dest[:], &bs):
		return false
	default:
		return true
	}
}

func (r *dhtRes) encode() []byte {
	coords := wire_encode_coords(r.coords)
	bs := wire_encode_uint64(wire_DHTLookupResponse)
	bs = append(bs, r.key[:]...)
	bs = append(bs, coords...)
	bs = append(bs, r.dest[:]...)
	for _, info := range r.infos {
		coords = wire_encode_coords(info.coords)
		bs = append(bs, info.key[:]...)
		bs = append(bs, coords...)
	}
	return bs
}

func (r *dhtRes) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_DHTLookupResponse:
		return false
	case !wire_chop_slice(r.key[:], &bs):
		return false
	case !wire_chop_coords(&r.coords, &bs):
		return false
	case !wire_chop_slice(r.dest[:], &bs):
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
		r.infos = append(r.infos, &info)
	}
	return true
}

////////////////////////////////////////////////////////////////////////////////

func (r *searchReq) encode() []byte {
	coords := wire_encode_coords(r.coords)
	bs := wire_encode_uint64(wire_SearchRequest)
	bs = append(bs, r.key[:]...)
	bs = append(bs, coords...)
	bs = append(bs, r.dest[:]...)
	return bs
}

func (r *searchReq) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_SearchRequest:
		return false
	case !wire_chop_slice(r.key[:], &bs):
		return false
	case !wire_chop_coords(&r.coords, &bs):
		return false
	case !wire_chop_slice(r.dest[:], &bs):
		return false
	default:
		return true
	}
}

func (r *searchRes) encode() []byte {
	coords := wire_encode_coords(r.coords)
	bs := wire_encode_uint64(wire_SearchResponse)
	bs = append(bs, r.key[:]...)
	bs = append(bs, coords...)
	bs = append(bs, r.dest[:]...)
	return bs
}

func (r *searchRes) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_SearchResponse:
		return false
	case !wire_chop_slice(r.key[:], &bs):
		return false
	case !wire_chop_coords(&r.coords, &bs):
		return false
	case !wire_chop_slice(r.dest[:], &bs):
		return false
	default:
		return true
	}
}
