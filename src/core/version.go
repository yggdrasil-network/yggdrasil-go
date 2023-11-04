package core

// This file contains the version metadata struct
// Used in the initial connection setup and key exchange
// Some of this could arguably go in wire.go instead

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
	"io"

	"golang.org/x/crypto/blake2b"
)

// This is the version-specific metadata exchanged at the start of a connection.
// It must always begin with the 4 bytes "meta" and a wire formatted uint64 major version number.
// The current version also includes a minor version number, and the box/sig/link keys that need to be exchanged to open a connection.
type version_metadata struct {
	majorVer  uint16
	minorVer  uint16
	publicKey ed25519.PublicKey
	priority  uint8
}

const (
	ProtocolVersionMajor uint16 = 0
	ProtocolVersionMinor uint16 = 5
)

// Once a major/minor version is released, it is not safe to change any of these
// (including their ordering), it is only safe to add new ones.
const (
	metaVersionMajor uint16 = iota // uint16
	metaVersionMinor               // uint16
	metaPublicKey                  // [32]byte
	metaPriority                   // uint8
)

// Gets a base metadata with no keys set, but with the correct version numbers.
func version_getBaseMetadata() version_metadata {
	return version_metadata{
		majorVer: ProtocolVersionMajor,
		minorVer: ProtocolVersionMinor,
	}
}

// Encodes version metadata into its wire format.
func (m *version_metadata) encode(privateKey ed25519.PrivateKey, password []byte) ([]byte, error) {
	bs := make([]byte, 0, 64)
	bs = append(bs, 'm', 'e', 't', 'a')
	bs = append(bs, 0, 0) // Remaining message length

	bs = binary.BigEndian.AppendUint16(bs, metaVersionMajor)
	bs = binary.BigEndian.AppendUint16(bs, 2)
	bs = binary.BigEndian.AppendUint16(bs, m.majorVer)

	bs = binary.BigEndian.AppendUint16(bs, metaVersionMinor)
	bs = binary.BigEndian.AppendUint16(bs, 2)
	bs = binary.BigEndian.AppendUint16(bs, m.minorVer)

	bs = binary.BigEndian.AppendUint16(bs, metaPublicKey)
	bs = binary.BigEndian.AppendUint16(bs, ed25519.PublicKeySize)
	bs = append(bs, m.publicKey[:]...)

	bs = binary.BigEndian.AppendUint16(bs, metaPriority)
	bs = binary.BigEndian.AppendUint16(bs, 1)
	bs = append(bs, m.priority)

	hasher, err := blake2b.New512(password)
	if err != nil {
		return nil, err
	}
	n, err := hasher.Write(m.publicKey)
	if err != nil {
		return nil, err
	}
	if n != ed25519.PublicKeySize {
		return nil, fmt.Errorf("hash writer only wrote %d bytes", n)
	}
	hash := hasher.Sum(nil)
	bs = append(bs, ed25519.Sign(privateKey, hash)...)

	binary.BigEndian.PutUint16(bs[4:6], uint16(len(bs)-6))
	return bs, nil
}

// Decodes version metadata from its wire format into the struct.
func (m *version_metadata) decode(r io.Reader, password []byte) error {
	bh := [6]byte{}
	if _, err := io.ReadFull(r, bh[:]); err != nil {
		return err
	}
	meta := [4]byte{'m', 'e', 't', 'a'}
	if !bytes.Equal(bh[:4], meta[:]) {
		return fmt.Errorf("invalid handshake preamble")
	}
	hl := binary.BigEndian.Uint16(bh[4:6])
	if hl < ed25519.SignatureSize {
		return fmt.Errorf("invalid handshake length")
	}
	bs := make([]byte, hl)
	if _, err := io.ReadFull(r, bs); err != nil {
		return err
	}
	sig := bs[len(bs)-ed25519.SignatureSize:]
	bs = bs[:len(bs)-ed25519.SignatureSize]

	for len(bs) >= 4 {
		op := binary.BigEndian.Uint16(bs[:2])
		oplen := binary.BigEndian.Uint16(bs[2:4])
		if bs = bs[4:]; len(bs) < int(oplen) {
			break
		}
		switch op {
		case metaVersionMajor:
			m.majorVer = binary.BigEndian.Uint16(bs[:2])

		case metaVersionMinor:
			m.minorVer = binary.BigEndian.Uint16(bs[:2])

		case metaPublicKey:
			m.publicKey = make(ed25519.PublicKey, ed25519.PublicKeySize)
			copy(m.publicKey, bs[:ed25519.PublicKeySize])

		case metaPriority:
			m.priority = bs[0]
		}
		bs = bs[oplen:]
	}

	hasher, err := blake2b.New512(password)
	if err != nil {
		return fmt.Errorf("invalid password supplied")
	}
	n, err := hasher.Write(m.publicKey)
	if err != nil || n != ed25519.PublicKeySize {
		return fmt.Errorf("failed to generate hash")
	}
	hash := hasher.Sum(nil)
	if !ed25519.Verify(m.publicKey, hash, sig) {
		return fmt.Errorf("password is incorrect")
	}
	return nil
}

// Checks that the "meta" bytes and the version numbers are the expected values.
func (m *version_metadata) check() bool {
	switch {
	case m.majorVer != ProtocolVersionMajor:
		return false
	case m.minorVer != ProtocolVersionMinor:
		return false
	case len(m.publicKey) != ed25519.PublicKeySize:
		return false
	default:
		return true
	}
}
