package multicast

import (
	"crypto/ed25519"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestMulticastAdvertisementRoundTrip(t *testing.T) {
	pk, sk, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	orig := multicastAdvertisement{
		MajorVersion: 1,
		MinorVersion: 2,
		PublicKey:    pk,
		Port:         3,
		Hash:         sk, // any bytes will do
	}

	ob, err := orig.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	var new multicastAdvertisement
	if err := new.UnmarshalBinary(ob); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(orig, new) {
		t.Logf("original: %+v", orig)
		t.Logf("new:      %+v", new)
		t.Fatalf("differences found after round-trip")
	}
}

func TestMulticastAdvertisementRejectsTruncatedHash(t *testing.T) {
	pk, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	b := make([]byte, ed25519.PublicKeySize+8)
	copy(b[4:], pk)
	binary.BigEndian.PutUint16(b[4+ed25519.PublicKeySize:6+ed25519.PublicKeySize], 9001)
	binary.BigEndian.PutUint16(b[6+ed25519.PublicKeySize:8+ed25519.PublicKeySize], 32)

	var adv multicastAdvertisement
	if err := adv.UnmarshalBinary(b); err == nil {
		t.Fatal("expected truncated beacon to be rejected")
	}
}
