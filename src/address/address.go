// Package address contains the types used by yggdrasil to represent IPv6 addresses or prefixes, as well as functions for working with these types.
// Of particular importance are the functions used to derive addresses or subnets from a NodeID, or to get the NodeID and bitmask of the bits visible from an address, which is needed for DHT searches.
package address

import (
	"fmt"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

// Address represents an IPv6 address in the yggdrasil address range.
type Address [16]byte

// Subnet represents an IPv6 /64 subnet in the yggdrasil subnet range.
type Subnet [8]byte

// GetPrefix returns the address prefix used by yggdrasil.
// The current implementation requires this to be a multiple of 8 bits + 7 bits.
// The 8th bit of the last byte is used to signal nodes (0) or /64 prefixes (1).
// Nodes that configure this differently will be unable to communicate with each other using IP packets, though routing and the DHT machinery *should* still work.
func GetPrefix() [1]byte {
	return [...]byte{0x02}
}

// IsValid returns true if an address falls within the range used by nodes in the network.
func (a *Address) IsValid() bool {
	prefix := GetPrefix()
	for idx := range prefix {
		if (*a)[idx] != prefix[idx] {
			return false
		}
	}
	return true
}

// IsValid returns true if a prefix falls within the range usable by the network.
func (s *Subnet) IsValid() bool {
	prefix := GetPrefix()
	l := len(prefix)
	for idx := range prefix[:l-1] {
		if (*s)[idx] != prefix[idx] {
			return false
		}
	}
	return (*s)[l-1] == prefix[l-1]|0x01
}

// AddrForNodeID takes a *NodeID as an argument and returns an *Address.
// This address begins with the contents of GetPrefix(), with the last bit set to 0 to indicate an address.
// The following 8 bits are set to the number of leading 1 bits in the NodeID.
// The NodeID, excluding the leading 1 bits and the first leading 0 bit, is truncated to the appropriate length and makes up the remainder of the address.
func AddrForNodeID(nid *crypto.NodeID) *Address {
	// 128 bit address
	// Begins with prefix
	// Next bit is a 0
	// Next 7 bits, interpreted as a uint, are # of leading 1s in the NodeID
	// Leading 1s and first leading 0 of the NodeID are truncated off
	// The rest is appended to the IPv6 address (truncated to 128 bits total)
	var addr Address
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
	prefix := GetPrefix()
	copy(addr[:], prefix[:])
	addr[len(prefix)] = ones
	copy(addr[len(prefix)+1:], temp)
	return &addr
}

// SubnetForNodeID takes a *NodeID as an argument and returns an *Address.
// This subnet begins with the address prefix, with the last bit set to 1 to indicate a prefix.
// The following 8 bits are set to the number of leading 1 bits in the NodeID.
// The NodeID, excluding the leading 1 bits and the first leading 0 bit, is truncated to the appropriate length and makes up the remainder of the subnet.
func SubnetForNodeID(nid *crypto.NodeID) *Subnet {
	// Exactly as the address version, with two exceptions:
	//  1) The first bit after the fixed prefix is a 1 instead of a 0
	//  2) It's truncated to a subnet prefix length instead of 128 bits
	addr := *AddrForNodeID(nid)
	var snet Subnet
	copy(snet[:], addr[:])
	prefix := GetPrefix()
	snet[len(prefix)-1] |= 0x01
	return &snet
}

// GetNodeIDandMask returns two *NodeID.
// The first is a NodeID with all the bits known from the Address set to their correct values.
// The second is a bitmask with 1 bit set for each bit that was known from the Address.
// This is used to look up NodeIDs in the DHT and tell if they match an Address.
func (a *Address) GetNodeIDandMask() (*crypto.NodeID, *crypto.NodeID) {
	// Mask is a bitmask to mark the bits visible from the address
	// This means truncated leading 1s, first leading 0, and visible part of addr
	var nid crypto.NodeID
	var mask crypto.NodeID
	prefix := GetPrefix()
	ones := int(a[len(prefix)])
	for idx := 0; idx < ones; idx++ {
		nid[idx/8] |= 0x80 >> byte(idx%8)
	}
	nidOffset := ones + 1
	addrOffset := 8*len(prefix) + 8
	for idx := addrOffset; idx < 8*len(a); idx++ {
		bits := a[idx/8] & (0x80 >> byte(idx%8))
		bits <<= byte(idx % 8)
		nidIdx := nidOffset + (idx - addrOffset)
		bits >>= byte(nidIdx % 8)
		nid[nidIdx/8] |= bits
	}
	maxMask := 8*(len(a)-len(prefix)-1) + ones + 1
	for idx := 0; idx < maxMask; idx++ {
		mask[idx/8] |= 0x80 >> byte(idx%8)
	}
	return &nid, &mask
}

// GetNodeIDLengthString returns a string representation of the known bits of the NodeID, along with the number of known bits, for use with yggdrasil.Dialer's Dial and DialContext functions.
func (a *Address) GetNodeIDLengthString() string {
	nid, mask := a.GetNodeIDandMask()
	l := mask.PrefixLength()
	return fmt.Sprintf("%s/%d", nid.String(), l)
}

// GetNodeIDandMask returns two *NodeID.
// The first is a NodeID with all the bits known from the Subnet set to their correct values.
// The second is a bitmask with 1 bit set for each bit that was known from the Subnet.
// This is used to look up NodeIDs in the DHT and tell if they match a Subnet.
func (s *Subnet) GetNodeIDandMask() (*crypto.NodeID, *crypto.NodeID) {
	// As with the address version, but visible parts of the subnet prefix instead
	var nid crypto.NodeID
	var mask crypto.NodeID
	prefix := GetPrefix()
	ones := int(s[len(prefix)])
	for idx := 0; idx < ones; idx++ {
		nid[idx/8] |= 0x80 >> byte(idx%8)
	}
	nidOffset := ones + 1
	addrOffset := 8*len(prefix) + 8
	for idx := addrOffset; idx < 8*len(s); idx++ {
		bits := s[idx/8] & (0x80 >> byte(idx%8))
		bits <<= byte(idx % 8)
		nidIdx := nidOffset + (idx - addrOffset)
		bits >>= byte(nidIdx % 8)
		nid[nidIdx/8] |= bits
	}
	maxMask := 8*(len(s)-len(prefix)-1) + ones + 1
	for idx := 0; idx < maxMask; idx++ {
		mask[idx/8] |= 0x80 >> byte(idx%8)
	}
	return &nid, &mask
}

// GetNodeIDLengthString returns a string representation of the known bits of the NodeID, along with the number of known bits, for use with yggdrasil.Dialer's Dial and DialContext functions.
func (s *Subnet) GetNodeIDLengthString() string {
	nid, mask := s.GetNodeIDandMask()
	l := mask.PrefixLength()
	return fmt.Sprintf("%s/%d", nid.String(), l)
}
