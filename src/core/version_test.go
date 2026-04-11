package core

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"reflect"
	"testing"

	"golang.org/x/crypto/blake2b"
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

func TestVersionDecodeRejectsMalformedFieldLengths(t *testing.T) {
	password := []byte("pw")
	for _, tt := range []struct {
		name  string
		op    uint16
		field []byte
	}{
		{name: "major short", op: metaVersionMajor, field: []byte{1}},
		{name: "minor short", op: metaVersionMinor, field: []byte{1}},
		{name: "public key short", op: metaPublicKey, field: []byte{1}},
		{name: "priority empty", op: metaPriority, field: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			msg := malformedVersionHandshake(t, tt.op, tt.field, password)
			var decoded version_metadata
			if err := decoded.decode(bytes.NewReader(msg), password); err != ErrHandshakeInvalidLength {
				t.Fatalf("expected %q, got %v", ErrHandshakeInvalidLength, err)
			}
		})
	}
}

func TestVersionDecodeRejectsTrailingBytes(t *testing.T) {
	password := []byte("pw")
	pk, sk, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	hasher, err := blake2b.New512(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = hasher.Write(pk); err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(sk, hasher.Sum(nil))

	body := append([]byte{1, 2, 3}, sig...)
	msg := append([]byte{'m', 'e', 't', 'a', 0, 0}, body...)
	binary.BigEndian.PutUint16(msg[4:6], uint16(len(body)))
	var decoded version_metadata
	if err := decoded.decode(bytes.NewReader(msg), password); err != ErrHandshakeInvalidLength {
		t.Fatalf("expected %q, got %v", ErrHandshakeInvalidLength, err)
	}
}

func malformedVersionHandshake(t *testing.T, op uint16, field []byte, password []byte) []byte {
	t.Helper()

	pk, sk, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	hasher, err := blake2b.New512(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = hasher.Write(pk); err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(sk, hasher.Sum(nil))

	body := make([]byte, 0, 4+len(field)+len(sig))
	body = binary.BigEndian.AppendUint16(body, op)
	body = binary.BigEndian.AppendUint16(body, uint16(len(field)))
	body = append(body, field...)
	body = append(body, sig...)

	msg := append([]byte{'m', 'e', 't', 'a', 0, 0}, body...)
	binary.BigEndian.PutUint16(msg[4:6], uint16(len(body)))
	return msg
}
