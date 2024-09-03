package core

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
)

type PeerInfoDB struct {
	PeerInfo
	Id          int
	CoordsBytes []byte
	KeyBytes    []byte
	RootBytes   []byte
	PeerErr     sql.NullString
}

type SelfInfoDB struct {
	SelfInfo
	Id       int
	KeyBytes []byte
}

type TreeEntryInfoDB struct {
	TreeEntryInfo
	Id          int
	KeyBytes    []byte
	ParentBytes []byte
}

type PathEntryInfoDB struct {
	PathEntryInfo
	Id        int
	KeyBytes  []byte
	PathBytes []byte
}

type SessionInfoDB struct {
	SessionInfo
	Id       int
	KeyBytes []byte
}

func NewPeerInfoDB(peerInfo PeerInfo) (_ *PeerInfoDB, err error) {
	peer := &PeerInfoDB{
		PeerInfo: peerInfo,
	}
	peer.PeerErr = MarshalError(peer.LastError)
	peer.CoordsBytes = ConvertToByteSlise(peer.Coords)
	peer.KeyBytes, err = MarshalPKIXPublicKey(&peer.Key)
	if err != nil {
		return nil, err
	}
	peer.RootBytes, err = MarshalPKIXPublicKey(&peer.Root)
	if err != nil {
		return nil, err
	}
	return peer, nil
}

func NewSelfInfoDB(selfinfo SelfInfo) (_ *SelfInfoDB, err error) {
	model := &SelfInfoDB{
		SelfInfo: selfinfo,
	}
	model.KeyBytes, err = MarshalPKIXPublicKey(&model.Key)
	if err != nil {
		return nil, err
	}
	return model, nil
}

func NewTreeEntryInfoDB(treeEntyInfo TreeEntryInfo) (_ *TreeEntryInfoDB, err error) {
	model := &TreeEntryInfoDB{
		TreeEntryInfo: treeEntyInfo,
	}
	model.KeyBytes, err = MarshalPKIXPublicKey(&model.Key)
	if err != nil {
		return nil, err
	}
	model.ParentBytes, err = MarshalPKIXPublicKey(&model.Parent)
	if err != nil {
		return nil, err
	}
	return model, nil
}

func NewPathEntryInfoDB(PathEntryInfo PathEntryInfo) (_ *PathEntryInfoDB, err error) {
	model := &PathEntryInfoDB{
		PathEntryInfo: PathEntryInfo,
	}
	model.KeyBytes, err = MarshalPKIXPublicKey(&model.Key)
	if err != nil {
		return nil, err
	}
	model.PathBytes = ConvertToByteSlise(model.Path)

	return model, nil
}

func NewSessionInfoDB(SessionInfo SessionInfo) (_ *SessionInfoDB, err error) {
	model := &SessionInfoDB{
		SessionInfo: SessionInfo,
	}
	model.KeyBytes, err = MarshalPKIXPublicKey(&model.Key)
	if err != nil {
		return nil, err
	}
	return model, nil
}

func ConvertToByteSlise(uintSlise []uint64) []byte {
	var ByteSlise []byte
	if uintSlise != nil {
		ByteSlise = make([]byte, len(uintSlise)*8)
		for i, coord := range uintSlise {
			binary.LittleEndian.PutUint64(ByteSlise[i*8:], coord)
		}
	}
	return ByteSlise
}

func ConvertToUintSlise(ByteSlise []byte) (_ []uint64, err error) {
	if len(ByteSlise)%8 != 0 {
		return nil, fmt.Errorf("length of byte slice must be a multiple of 8")
	}
	var uintSlise []uint64
	length := len(ByteSlise) / 8
	uintSlise = make([]uint64, length)
	reader := bytes.NewReader(ByteSlise)
	for i := 0; i < length; i++ {
		err := binary.Read(reader, binary.LittleEndian, &uintSlise[i])
		if err != nil {
			return nil, err
		}
	}
	return uintSlise, nil
}

func MarshalPKIXPublicKey(PublicKey *ed25519.PublicKey) ([]byte, error) {
	pkey, err := x509.MarshalPKIXPublicKey(*PublicKey)
	if err != nil {
		return nil, err
	}
	return pkey, nil
}

func ParsePKIXPublicKey(derBytes *[]byte) (PublicKey ed25519.PublicKey, err error) {
	key, err := x509.ParsePKIXPublicKey(*derBytes)
	if err != nil {
		return nil, err
	}
	return key.(ed25519.PublicKey), nil
}

func ParseError(PeerErr sql.NullString) error {
	if PeerErr.Valid {
		return errors.New(PeerErr.String)
	}
	return nil
}

func MarshalError(err error) sql.NullString {
	if err != nil {
		return sql.NullString{
			String: err.Error(),
			Valid:  true,
		}
	} else {
		return sql.NullString{
			String: "",
			Valid:  false,
		}
	}
}
