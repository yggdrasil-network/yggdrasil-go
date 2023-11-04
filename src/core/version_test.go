package core

import (
	"bytes"
	"crypto/ed25519"
	"reflect"
	"testing"
)

func TestVersionPasswordAuth(t *testing.T) {
	for _, tt := range []struct {
		password1 []byte // The password on node 1
		password2 []byte // The password on node 2
		allowed   bool   // Should the connection have been allowed?
	}{
		{nil, nil, true},                      // Allow:  No passwords (both nil)
		{nil, []byte(""), true},               // Allow:  No passwords (mixed nil and empty string)
		{nil, []byte("foo"), false},           // Reject: One node has a password, the other doesn't
		{[]byte("foo"), []byte(""), false},    // Reject: One node has a password, the other doesn't
		{[]byte("foo"), []byte("foo"), true},  // Allow:  Same password
		{[]byte("foo"), []byte("bar"), false}, // Reject: Different passwords
	} {
		pk1, sk1, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("Node 1 failed to generate key: %s", err)
		}

		metadata1 := &version_metadata{
			publicKey: pk1,
		}
		encoded, err := metadata1.encode(sk1, tt.password1)
		if err != nil {
			t.Fatalf("Node 1 failed to encode metadata: %s", err)
		}

		var decoded version_metadata
		if allowed := decoded.decode(bytes.NewBuffer(encoded), tt.password2) == nil; allowed != tt.allowed {
			t.Fatalf("Permutation %q -> %q should have been %v but was %v", tt.password1, tt.password2, tt.allowed, allowed)
		}
	}
}

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
			if err := decoded.decode(encoded, password); err != nil {
				t.Fatalf("failed to decode: %s", err)
			}
			if !reflect.DeepEqual(test, decoded) {
				t.Fatalf("round-trip failed\nwant: %+v\n got: %+v", test, decoded)
			}
		}
	}
}
