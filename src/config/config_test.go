package config

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"strings"
	"testing"
)

// ReadFrom previously sliced conf[0:2] for the BOM check without
// guarding the length, so empty or single-byte configs piped via
// -useconf panicked with index out of range.
func TestConfigReadFromEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{name: "empty", body: nil},
		{name: "single byte", body: []byte{'{'}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ReadFrom must not panic on short input, got: %v", r)
				}
			}()
			var cfg NodeConfig
			_, _ = cfg.ReadFrom(bytes.NewReader(tc.body))
		})
	}
}

func TestConfig_Keys(t *testing.T) {
	/*
		var nodeConfig NodeConfig
		nodeConfig.NewKeys()

		publicKey1, err := hex.DecodeString(nodeConfig.PublicKey)

		if err != nil {
			t.Fatal("can not decode generated public key")
		}

		if len(publicKey1) == 0 {
			t.Fatal("empty public key generated")
		}

		privateKey1, err := hex.DecodeString(nodeConfig.PrivateKey)

		if err != nil {
			t.Fatal("can not decode generated private key")
		}

		if len(privateKey1) == 0 {
			t.Fatal("empty private key generated")
		}

		nodeConfig.NewKeys()

		publicKey2, err := hex.DecodeString(nodeConfig.PublicKey)

		if err != nil {
			t.Fatal("can not decode generated public key")
		}

		if bytes.Equal(publicKey2, publicKey1) {
			t.Fatal("same public key generated")
		}

		privateKey2, err := hex.DecodeString(nodeConfig.PrivateKey)

		if err != nil {
			t.Fatal("can not decode generated private key")
		}

		if bytes.Equal(privateKey2, privateKey1) {
			t.Fatal("same private key generated")
		}
	*/
}

// A private key shorter than 32 bytes previously reached
// x509.MarshalPKCS8PrivateKey -> ed25519.PrivateKey.Seed and panicked with
// "slice bounds out of range [:32]". A config file is untrusted input, so it
// must produce an error instead of taking the process down (on mobile this
// happens inside StartJSON, i.e. it crashes the application).
func TestConfigRejectsIncorrectPrivateKeyLength(t *testing.T) {
	for _, tc := range []struct {
		name      string
		hexKey    string
		wantBytes int
	}{
		{name: "one byte", hexKey: "aa", wantBytes: 1},
		{name: "reported case", hexKey: "aabb", wantBytes: 2},
		{name: "seed size", hexKey: strings.Repeat("aa", ed25519.SeedSize), wantBytes: ed25519.SeedSize},
		{name: "one byte short", hexKey: strings.Repeat("aa", ed25519.PrivateKeySize-1), wantBytes: ed25519.PrivateKeySize - 1},
		{name: "one byte long", hexKey: strings.Repeat("aa", ed25519.PrivateKeySize+1), wantBytes: ed25519.PrivateKeySize + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("config parsing panicked on a %d byte private key: %v", tc.wantBytes, r)
				}
			}()
			want := fmt.Sprintf("private key is incorrect length: got %d bytes, want %d", tc.wantBytes, ed25519.PrivateKeySize)

			var cfg NodeConfig
			_, err := cfg.ReadFrom(strings.NewReader(fmt.Sprintf(`{"PrivateKey":%q}`, tc.hexKey)))
			if err == nil {
				t.Fatalf("ReadFrom accepted a %d byte private key", tc.wantBytes)
			}
			if err.Error() != want {
				t.Fatalf("ReadFrom error = %q, want %q", err, want)
			}

			// UnmarshalHJSON is the entry point used by the mobile package.
			cfg = *GenerateConfig()
			err = cfg.UnmarshalHJSON([]byte(fmt.Sprintf(`{"PrivateKey":%q}`, tc.hexKey)))
			if err == nil {
				t.Fatalf("UnmarshalHJSON accepted a %d byte private key", tc.wantBytes)
			}
			if err.Error() != want {
				t.Fatalf("UnmarshalHJSON error = %q, want %q", err, want)
			}
		})
	}
}

// An empty private key means "no key was supplied", so a new one (and a new
// self-signed certificate) must be generated instead of panicking.
func TestConfigEmptyPrivateKeyGeneratesNewKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "empty string", body: `{"PrivateKey":""}`},
		{name: "missing", body: `{}`},
		{name: "null", body: `{"PrivateKey":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("config parsing panicked: %v", r)
				}
			}()
			var cfg NodeConfig
			if _, err := cfg.ReadFrom(strings.NewReader(tc.body)); err != nil {
				t.Fatalf("ReadFrom: %s", err)
			}
			if len(cfg.PrivateKey) != ed25519.PrivateKeySize {
				t.Fatalf("private key has %d bytes, want %d", len(cfg.PrivateKey), ed25519.PrivateKeySize)
			}
			if cfg.Certificate == nil {
				t.Fatal("no certificate was generated")
			}
			certKey, ok := cfg.Certificate.PrivateKey.(ed25519.PrivateKey)
			if !ok {
				t.Fatalf("certificate key has type %T, want ed25519.PrivateKey", cfg.Certificate.PrivateKey)
			}
			if !bytes.Equal(certKey, cfg.PrivateKey) {
				t.Fatal("certificate does not match the generated private key")
			}
		})
	}
}
