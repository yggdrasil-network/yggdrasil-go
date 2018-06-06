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
	if coordBegin == 0 || coordEnd > len(packet) {
		return nil, 0
	}
	return packet[coordBegin:coordEnd], coordEnd
}

////////////////////////////////////////////////////////////////////////////////

// Announces that we can send parts of a Message with a particular seq
type msgAnnounce struct {
	Root   sigPubKey
	Tstamp int64
	Seq    uint64
	Len    uint64
	//Deg uint64
	Rseq uint64
}

func (m *msgAnnounce) encode() []byte {
	bs := wire_encode_uint64(wire_SwitchAnnounce)
	bs = append(bs, m.Root[:]...)
	bs = append(bs, wire_encode_uint64(wire_intToUint(m.Tstamp))...)
	bs = append(bs, wire_encode_uint64(m.Seq)...)
	bs = append(bs, wire_encode_uint64(m.Len)...)
	bs = append(bs, wire_encode_uint64(m.Rseq)...)
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
	case !wire_chop_slice(m.Root[:], &bs):
		return false
	case !wire_chop_uint64(&tstamp, &bs):
		return false
	case !wire_chop_uint64(&m.Seq, &bs):
		return false
	case !wire_chop_uint64(&m.Len, &bs):
		return false
	case !wire_chop_uint64(&m.Rseq, &bs):
		return false
	}
	m.Tstamp = wire_intFromUint(tstamp)
	return true
}

type msgHopReq struct {
	Root   sigPubKey
	Tstamp int64
	Seq    uint64
	Hop    uint64
}

func (m *msgHopReq) encode() []byte {
	bs := wire_encode_uint64(wire_SwitchHopRequest)
	bs = append(bs, m.Root[:]...)
	bs = append(bs, wire_encode_uint64(wire_intToUint(m.Tstamp))...)
	bs = append(bs, wire_encode_uint64(m.Seq)...)
	bs = append(bs, wire_encode_uint64(m.Hop)...)
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
	case !wire_chop_slice(m.Root[:], &bs):
		return false
	case !wire_chop_uint64(&tstamp, &bs):
		return false
	case !wire_chop_uint64(&m.Seq, &bs):
		return false
	case !wire_chop_uint64(&m.Hop, &bs):
		return false
	}
	m.Tstamp = wire_intFromUint(tstamp)
	return true
}

type msgHop struct {
	Root   sigPubKey
	Tstamp int64
	Seq    uint64
	Hop    uint64
	Port   switchPort
	Next   sigPubKey
	Sig    sigBytes
}

func (m *msgHop) encode() []byte {
	bs := wire_encode_uint64(wire_SwitchHop)
	bs = append(bs, m.Root[:]...)
	bs = append(bs, wire_encode_uint64(wire_intToUint(m.Tstamp))...)
	bs = append(bs, wire_encode_uint64(m.Seq)...)
	bs = append(bs, wire_encode_uint64(m.Hop)...)
	bs = append(bs, wire_encode_uint64(uint64(m.Port))...)
	bs = append(bs, m.Next[:]...)
	bs = append(bs, m.Sig[:]...)
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
	case !wire_chop_slice(m.Root[:], &bs):
		return false
	case !wire_chop_uint64(&tstamp, &bs):
		return false
	case !wire_chop_uint64(&m.Seq, &bs):
		return false
	case !wire_chop_uint64(&m.Hop, &bs):
		return false
	case !wire_chop_uint64((*uint64)(&m.Port), &bs):
		return false
	case !wire_chop_slice(m.Next[:], &bs):
		return false
	case !wire_chop_slice(m.Sig[:], &bs):
		return false
	}
	m.Tstamp = wire_intFromUint(tstamp)
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
	TTL     uint64
	Coords  []byte
	Handle  handle
	Nonce   boxNonce
	Payload []byte
}

// This is basically MarshalBinary, but decode doesn't allow that...
func (p *wire_trafficPacket) encode() []byte {
	bs := util_getBytes()
	bs = wire_put_uint64(wire_Traffic, bs)
	bs = wire_put_uint64(p.TTL, bs)
	bs = wire_put_coords(p.Coords, bs)
	bs = append(bs, p.Handle[:]...)
	bs = append(bs, p.Nonce[:]...)
	bs = append(bs, p.Payload...)
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
	case !wire_chop_uint64(&p.TTL, &bs):
		return false
	case !wire_chop_coords(&p.Coords, &bs):
		return false
	case !wire_chop_slice(p.Handle[:], &bs):
		return false
	case !wire_chop_slice(p.Nonce[:], &bs):
		return false
	}
	p.Payload = append(util_getBytes(), bs...)
	return true
}

type wire_protoTrafficPacket struct {
	TTL     uint64
	Coords  []byte
	ToKey   boxPubKey
	FromKey boxPubKey
	Nonce   boxNonce
	Payload []byte
}

func (p *wire_protoTrafficPacket) encode() []byte {
	coords := wire_encode_coords(p.Coords)
	bs := wire_encode_uint64(wire_ProtocolTraffic)
	bs = append(bs, wire_encode_uint64(p.TTL)...)
	bs = append(bs, coords...)
	bs = append(bs, p.ToKey[:]...)
	bs = append(bs, p.FromKey[:]...)
	bs = append(bs, p.Nonce[:]...)
	bs = append(bs, p.Payload...)
	return bs
}

func (p *wire_protoTrafficPacket) decode(bs []byte) bool {
	var pType uint64
	switch {
	case !wire_chop_uint64(&pType, &bs):
		return false
	case pType != wire_ProtocolTraffic:
		return false
	case !wire_chop_uint64(&p.TTL, &bs):
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

type wire_linkProtoTrafficPacket struct {
	Nonce   boxNonce
	Payload []byte
}

func (p *wire_linkProtoTrafficPacket) encode() []byte {
	bs := wire_encode_uint64(wire_LinkProtocolTraffic)
	bs = append(bs, p.Nonce[:]...)
	bs = append(bs, p.Payload...)
	return bs
}

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
	p.MTU = uint16(mtu)
	return true
}

////////////////////////////////////////////////////////////////////////////////

func (r *dhtReq) encode() []byte {
	coords := wire_encode_coords(r.Coords)
	bs := wire_encode_uint64(wire_DHTLookupRequest)
	bs = append(bs, coords...)
	bs = append(bs, r.Dest[:]...)
	return bs
}

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

