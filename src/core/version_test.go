package core

import (
	"bytes"
	"crypto/ed25519"
	"reflect"
	"testing"
)

func TestVersionRoundtrip(t *testing.T) {
	for _, password := range [][]byte{
		nil, []byte(""), []byte("foo"),
	} {
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
			pk, sk, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}

			test.publicKey = pk
			meta, err := test.encode(sk, password)
			if err != nil {
				t.Fatal(err)
			}
			encoded := bytes.NewBuffer(meta)
			decoded := &version_metadata{}
			if !decoded.decode(encoded, password) {
				t.Fatalf("failed to decode")
			}
			if !reflect.DeepEqual(test, decoded) {
				t.Fatalf("round-trip failed\nwant: %+v\n got: %+v", test, decoded)
			}
		}
	}
}
