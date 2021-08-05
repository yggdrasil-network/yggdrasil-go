package address

import (
	"crypto/ed25519"
	"math/rand"
	"testing"
)

func TestAddress_Address_IsValid(t *testing.T) {
	var address Address
	rand.Read(address[:])

	address[0] = 0

	if address.IsValid() {
		t.Fatal("invalid address marked as valid")
	}

	address[0] = 0x03

	if address.IsValid() {
		t.Fatal("invalid address marked as valid")
	}

	address[0] = 0x02

	if !address.IsValid() {
		t.Fatal("valid address marked as invalid")
	}
}

func TestAddress_Subnet_IsValid(t *testing.T) {
	var subnet Subnet
	rand.Read(subnet[:])

	subnet[0] = 0

	if subnet.IsValid() {
		t.Fatal("invalid subnet marked as valid")
	}

	subnet[0] = 0x02

	if subnet.IsValid() {
		t.Fatal("invalid subnet marked as valid")
	}

	subnet[0] = 0x03

	if !subnet.IsValid() {
		t.Fatal("valid subnet marked as invalid")
	}
}

func TestAddress_AddrForKey(t *testing.T) {
	publicKey := ed25519.PublicKey{
		189, 186, 207, 216, 34, 64, 222, 61, 205, 18, 57, 36, 203, 181, 82, 86,
		251, 141, 171, 8, 170, 152, 227, 5, 82, 138, 184, 79, 65, 158, 110, 251,
	}

	gotAddress := AddrForKey(publicKey)

	expectedAddress := Address{
		2, 0, 132, 138, 96, 79, 187, 126, 67, 132, 101, 219, 141, 182, 104, 149,
	}

	if *gotAddress != expectedAddress {
		t.Fatal("invalid address returned")
	}
}
