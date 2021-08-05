package address

import (
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
