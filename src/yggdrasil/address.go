package yggdrasil

type address [16]byte // IPv6 address within the network
type subnet [8]byte   // It's a /64

var address_prefix = [...]byte{0xfd} // For node addresses + local subnets

func (a *address) isValid() bool {
	for idx := range address_prefix {
		if (*a)[idx] != address_prefix[idx] {
			return false
		}
	}
	return (*a)[len(address_prefix)]&0x80 == 0
}

func (s *subnet) isValid() bool {
	for idx := range address_prefix {
		if (*s)[idx] != address_prefix[idx] {
			return false
		}
	}
	return (*s)[len(address_prefix)]&0x80 != 0
}

func address_addrForNodeID(nid *NodeID) *address {
	// 128 bit address
	// Begins with prefix
	// Next bit is a 0
	// Next 7 bits, interpreted as a uint, are # of leading 1s in the NodeID
	// Leading 1s and first leading 0 of the NodeID are truncated off
	// The rest is appended to the IPv6 address (truncated to 128 bits total)
	var addr address
	var temp []byte
	done := false
	ones := byte(0)
	bits := byte(0)
	nBits := 0
	for idx := 0; idx < 8*len(nid); idx++ {
		bit := (nid[idx/8] & (0x80 >> byte(idx%8))) >> byte(7-(idx%8))
		if !done && bit != 0 {
			ones++
			continue
		}
		if !done && bit == 0 {
			done = true
			continue // FIXME this assumes that ones <= 127
		}
		bits = (bits << 1) | bit
		nBits++
		if nBits == 8 {
			nBits = 0
			temp = append(temp, bits)
		}
	}
	copy(addr[:], address_prefix[:])
	addr[len(address_prefix)] = ones & 0x7f
	copy(addr[len(address_prefix)+1:], temp)
	return &addr
}

func address_subnetForNodeID(nid *NodeID) *subnet {
	// Exactly as the address version, with two exceptions:
	//  1) The first bit after the fixed prefix is a 1 instead of a 0
	//  2) It's truncated to a subnet prefix length instead of 128 bits
	addr := *address_addrForNodeID(nid)
	var snet subnet
	copy(snet[:], addr[:])
	snet[len(address_prefix)] |= 0x80
	return &snet
}

func (a *address) getNodeIDandMask() (*NodeID, *NodeID) {
	// Mask is a bitmask to mark the bits visible from the address
	// This means truncated leading 1s, first leading 0, and visible part of addr
	var nid NodeID
	var mask NodeID
	ones := int(a[len(address_prefix)] & 0x7f)
	for idx := 0; idx < ones; idx++ {
		nid[idx/8] |= 0x80 >> byte(idx%8)
	}
	nidOffset := ones + 1
	addrOffset := 8*len(address_prefix) + 8
	for idx := addrOffset; idx < 8*len(a); idx++ {
		bits := a[idx/8] & (0x80 >> byte(idx%8))
		bits <<= byte(idx % 8)
		nidIdx := nidOffset + (idx - addrOffset)
		bits >>= byte(nidIdx % 8)
		nid[nidIdx/8] |= bits
	}
	maxMask := 8*(len(a)-len(address_prefix)-1) + ones + 1
	for idx := 0; idx < maxMask; idx++ {
		mask[idx/8] |= 0x80 >> byte(idx%8)
	}
	return &nid, &mask
}

func (s *subnet) getNodeIDandMask() (*NodeID, *NodeID) {
	// As with the address version, but visible parts of the subnet prefix instead
	var nid NodeID
	var mask NodeID
	ones := int(s[len(address_prefix)] & 0x7f)
	for idx := 0; idx < ones; idx++ {
		nid[idx/8] |= 0x80 >> byte(idx%8)
	}
	nidOffset := ones + 1
	addrOffset := 8*len(address_prefix) + 8
	for idx := addrOffset; idx < 8*len(s); idx++ {
		bits := s[idx/8] & (0x80 >> byte(idx%8))
		bits <<= byte(idx % 8)
		nidIdx := nidOffset + (idx - addrOffset)
		bits >>= byte(nidIdx % 8)
		nid[nidIdx/8] |= bits
	}
	maxMask := 8*(len(s)-len(address_prefix)-1) + ones + 1
	for idx := 0; idx < maxMask; idx++ {
		mask[idx/8] |= 0x80 >> byte(idx%8)
	}
	return &nid, &mask
}
