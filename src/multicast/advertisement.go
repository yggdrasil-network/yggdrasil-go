package multicast

import (
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
)

type multicastAdvertisement struct {
	MajorVersion  uint16
	MinorVersion  uint16
	PublicKey     ed25519.PublicKey
	Port          uint16
	Discriminator []byte
}

func (m *multicastAdvertisement) MarshalBinary() ([]byte, error) {
	b := make([]byte, 0, ed25519.PublicKeySize+8+len(m.Discriminator))
	b = binary.BigEndian.AppendUint16(b, m.MajorVersion)
	b = binary.BigEndian.AppendUint16(b, m.MinorVersion)
	b = append(b, m.PublicKey...)
	b = binary.BigEndian.AppendUint16(b, m.Port)
	b = binary.BigEndian.AppendUint16(b, uint16(len(m.Discriminator)))
	b = append(b, m.Discriminator...)
	return b, nil
}

func (m *multicastAdvertisement) UnmarshalBinary(b []byte) error {
	if len(b) < ed25519.PublicKeySize+8 {
		return fmt.Errorf("invalid multicast beacon")
	}
	m.MajorVersion = binary.BigEndian.Uint16(b[0:2])
	m.MinorVersion = binary.BigEndian.Uint16(b[2:4])
	m.PublicKey = append(m.PublicKey[:0], b[4:4+ed25519.PublicKeySize]...)
	m.Port = binary.BigEndian.Uint16(b[4+ed25519.PublicKeySize : 6+ed25519.PublicKeySize])
	dl := binary.BigEndian.Uint16(b[6+ed25519.PublicKeySize : 8+ed25519.PublicKeySize])
	m.Discriminator = append(m.Discriminator[:0], b[8+ed25519.PublicKeySize:8+ed25519.PublicKeySize+dl]...)
	return nil
}
