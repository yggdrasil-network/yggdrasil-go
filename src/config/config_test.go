package config

import (
	"bytes"
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
