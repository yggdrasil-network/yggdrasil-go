package core

import (
	"bytes"
	"crypto/ed25519"
	"math/rand"
	"testing"
)

func (c *Core) TestAddress_Address_IsValid(t *testing.T) {
	var address Address
	rand.Read(address[:])

	address[0] = 0

	if c.IsValidAddress(address) {
		t.Fatal("invalid address marked as valid")
	}

	address[0] = 0xfd

	if c.IsValidAddress(address) {
		t.Fatal("invalid address marked as valid")
	}

	address[0] = 0xfc

	if !c.IsValidAddress(address) {
		t.Fatal("valid address marked as invalid")
	}
}

func (c *Core) TestAddress_Subnet_IsValid(t *testing.T) {
	var subnet Subnet
	rand.Read(subnet[:])

	subnet[0] = 0

	if c.IsValidSubnet(subnet) {
		t.Fatal("invalid subnet marked as valid")
	}

	subnet[0] = 0xfc

	if c.IsValidSubnet(subnet) {
		t.Fatal("invalid subnet marked as valid")
	}

	subnet[0] = 0xfd

	if !c.IsValidSubnet(subnet) {
		t.Fatal("valid subnet marked as invalid")
	}
}

func (c *Core) TestAddress_AddrForKey(t *testing.T) {
	publicKey := ed25519.PublicKey{
		189, 186, 207, 216, 34, 64, 222, 61, 205, 18, 57, 36, 203, 181, 82, 86,
		251, 141, 171, 8, 170, 152, 227, 5, 82, 138, 184, 79, 65, 158, 110, 251,
	}

	expectedAddress := Address{
		0xfc, 0, 132, 138, 96, 79, 187, 126, 67, 132, 101, 219, 141, 182, 104, 149,
	}

	if *c.AddrForKey(publicKey) != expectedAddress {
		t.Fatal("invalid address returned")
	}
}

func (c *Core) TestAddress_SubnetForKey(t *testing.T) {
	publicKey := ed25519.PublicKey{
		189, 186, 207, 216, 34, 64, 222, 61, 205, 18, 57, 36, 203, 181, 82, 86,
		251, 141, 171, 8, 170, 152, 227, 5, 82, 138, 184, 79, 65, 158, 110, 251,
	}

	expectedSubnet := Subnet{0xfd, 0, 132, 138, 96, 79, 187, 126}

	if *c.SubnetForKey(publicKey) != expectedSubnet {
		t.Fatal("invalid subnet returned")
	}
}

func (c *Core) TestAddress_Address_GetKey(t *testing.T) {
	address := Address{
		0xfc, 0, 132, 138, 96, 79, 187, 126, 67, 132, 101, 219, 141, 182, 104, 149,
	}

	expectedPublicKey := ed25519.PublicKey{
		189, 186, 207, 216, 34, 64, 222, 61,
		205, 18, 57, 36, 203, 181, 127, 255,
		255, 255, 255, 255, 255, 255, 255, 255,
		255, 255, 255, 255, 255, 255, 255, 255,
	}

	if !bytes.Equal(c.GetAddressKey(address), expectedPublicKey) {
		t.Fatal("invalid public key returned")
	}
}

func (c *Core) TestAddress_Subnet_GetKey(t *testing.T) {
	subnet := Subnet{0xfd, 0, 132, 138, 96, 79, 187, 126}

	expectedPublicKey := ed25519.PublicKey{
		189, 186, 207, 216, 34, 64, 255, 255,
		255, 255, 255, 255, 255, 255, 255, 255,
		255, 255, 255, 255, 255, 255, 255, 255,
		255, 255, 255, 255, 255, 255, 255, 255,
	}

	if !bytes.Equal(c.GetSubnetKey(subnet), expectedPublicKey) {
		t.Fatal("invalid public key returned")
	}
}
