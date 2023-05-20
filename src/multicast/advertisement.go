package multicast

import (
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
)

type multicastAdvertisement struct {
	PublicKey ed25519.PublicKey
	Port      uint16
}

func (m *multicastAdvertisement) MarshalBinary() ([]byte, error) {
	b := make([]byte, 0, ed25519.PublicKeySize+2)
	b = append(b, m.PublicKey...)
	b = binary.BigEndian.AppendUint16(b, m.Port)
	return b, nil
}

func (m *multicastAdvertisement) UnmarshalBinary(b []byte) error {
	if len(b) < ed25519.PublicKeySize+2 {
		return fmt.Errorf("invalid multicast beacon")
	}
	m.PublicKey = b[:ed25519.PublicKeySize]
	m.Port = binary.BigEndian.Uint16(b[ed25519.PublicKeySize:])
	return nil
}
