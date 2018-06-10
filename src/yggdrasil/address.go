package yggdrasil

// address represents an IPv6 address in the yggdrasil address range.
type address [16]byte

// subnet represents an IPv6 /64 subnet in the yggdrasil subnet range.
type subnet [8]byte

// address_prefix is the prefix used for all addresses and subnets in the network.
// The current implementation requires this to be a multiple of 8 bits.
// Nodes that configure this differently will be unable to communicate with eachother, though routing and the DHT machinery *should* still work.
var address_prefix = [...]byte{0xfd}

// isValid returns true if an address falls within the range used by nodes in the network.
func (a *address) isValid() bool {
	for idx := range address_prefix {
		if (*a)[idx] != address_prefix[idx] {
			return false
		}
	}
	return (*a)[len(address_prefix)]&0x80 == 0
}

// isValid returns true if a prefix falls within the range usable by the network.
func (s *subnet) isValid() bool {
	for idx := range address_prefix {
		if (*s)[idx] != address_prefix[idx] {
			return false
		}
	}
	return (*s)[len(address_prefix)]&0x80 != 0
}

// address_addrForNodeID takes a *NodeID as an argument and returns an *address.
// This address begins with the address prefix.
// The next bit is 0 for an address, and 1 for a subnet.
// The following 7 bits are set to the number of leading 1 bits in the NodeID.
// The NodeID, excluding the leading 1 bits and the first leading 1 bit, is truncated to the appropriate length and makes up the remainder of the address.
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
			continue // FIXME? this assumes that ones <= 127, probably only worth changing by using a variable length uint64, but that would require changes to the addressing scheme, and I'm not sure ones > 127 is realistic
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

// address_subnetForNodeID takes a *NodeID as an argument and returns a *subnet.
// This subnet begins with the address prefix.
// The next bit is 0 for an address, and 1 for a subnet.
// The following 7 bits are set to the number of leading 1 bits in the NodeID.
// The NodeID, excluding the leading 1 bits and the first leading 1 bit, is truncated to the appropriate length and makes up the remainder of the subnet.
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

// getNodeIDandMask returns two *NodeID.
// The first is a NodeID with all the bits known from the address set to their correct values.
// The second is a bitmask with 1 bit set for each bit that was known from the address.
// This is used to look up NodeIDs in the DHT and tell if they match an address.
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

// getNodeIDandMask returns two *NodeID.
// The first is a NodeID with all the bits known from the address set to their correct values.
// The second is a bitmask with 1 bit set for each bit that was known from the subnet.
// This is used to look up NodeIDs in the DHT and tell if they match a subnet.
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
