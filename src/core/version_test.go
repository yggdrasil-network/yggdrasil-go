package core

import (
	"crypto/ed25519"
	"math/rand"
	"reflect"
	"testing"
)

func TestVersionRoundtrip(t *testing.T) {
	for _, test := range []*version_metadata{
		{majorVer: 1},
		{majorVer: 256},
		{majorVer: 2, minorVer: 4},
		{majorVer: 2, minorVer: 257},
		{majorVer: 258, minorVer: 259},
		{majorVer: 3, minorVer: 5, priority: 6},
		{majorVer: 260, minorVer: 261, priority: 7},
	} {
		// Generate a random public key for each time, since it is
		// a required field.
		test.publicKey = make(ed25519.PublicKey, ed25519.PublicKeySize)
		rand.Read(test.publicKey)

		encoded := test.encode()
		decoded := &version_metadata{}
		if !decoded.decode(encoded) {
			t.Fatalf("failed to decode")
		}
		if !reflect.DeepEqual(test, decoded) {
			t.Fatalf("round-trip failed\nwant: %+v\n got: %+v", test, decoded)
		}
	}
}
