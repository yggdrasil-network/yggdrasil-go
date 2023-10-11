package multicast

import (
	"crypto/ed25519"
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
