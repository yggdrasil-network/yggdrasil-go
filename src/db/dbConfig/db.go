package db

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

type PeerInfoDB struct {
	core.PeerInfo
	Id            int
	Coords        Blob
	Key           PublicKeyContainer
	Root          PublicKeyContainer
	Error         ErrorContainer
	LastErrorTime sql.NullTime
}

type SelfInfoDB struct {
	core.SelfInfo
	Id  int
	Key PublicKeyContainer
}

type TreeEntryInfoDB struct {
	core.TreeEntryInfo
	Id     int
	Key    PublicKeyContainer
	Parent PublicKeyContainer
	TreeId int
}

type PathEntryInfoDB struct {
	core.PathEntryInfo
	Id   int
	Key  PublicKeyContainer
	Path Blob
}

type SessionInfoDB struct {
	core.SessionInfo
	Id  int
	Key PublicKeyContainer
}

func NewPeerInfoDB(peerInfo core.PeerInfo) (_ *PeerInfoDB, err error) {
	peer := &PeerInfoDB{
		PeerInfo: peerInfo,
	}
	peer.Error = ErrorContainer{
		_err: peerInfo.LastError,
	}
	peer.Error.ParseError(peerInfo.LastError)

	peer.Coords = Blob{
		_uint: peerInfo.Coords,
	}
	peer.Coords.ParseUint64Sliсe(peer.Coords._uint)

	peer.Key = PublicKeyContainer{
		_publicKey: peerInfo.Key,
	}
	publicKey := peer.Key.GetPKIXPublicKey()
	peer.Key.MarshalPKIXPublicKey(&publicKey)

	peer.Root = PublicKeyContainer{
		_publicKey: peerInfo.Root,
	}
	publicKey = peer.Root.GetPKIXPublicKey()
	peer.Root.MarshalPKIXPublicKey(&publicKey)
	peer.LastErrorTime = sql.NullTime{}
	if !peerInfo.LastErrorTime.IsZero() {
		peer.LastErrorTime.Time = peerInfo.LastErrorTime
		peer.LastErrorTime.Valid = true
	} else {
		peer.LastErrorTime.Valid = false
	}
	return peer, nil
}

func NewSelfInfoDB(selfinfo core.SelfInfo) (_ *SelfInfoDB, err error) {
	model := &SelfInfoDB{
		SelfInfo: selfinfo,
	}
	model.Key = PublicKeyContainer{
		_publicKey: selfinfo.Key,
	}
	publicKey := model.Key.GetPKIXPublicKey()
	model.Key.MarshalPKIXPublicKey(&publicKey)
	if err != nil {
		return nil, err
	}
	return model, nil
}

func NewTreeEntryInfoDB(treeEntyInfo core.TreeEntryInfo) (_ *TreeEntryInfoDB, err error) {
	model := &TreeEntryInfoDB{
		TreeEntryInfo: treeEntyInfo,
	}
	model.Key = PublicKeyContainer{
		_publicKey: treeEntyInfo.Key,
	}
	publicKey := model.Key.GetPKIXPublicKey()
	model.Key.MarshalPKIXPublicKey(&publicKey)
	model.Parent = PublicKeyContainer{
		_publicKey: treeEntyInfo.Parent,
	}
	publicKey = model.Parent.GetPKIXPublicKey()
	model.Parent.MarshalPKIXPublicKey(&publicKey)
	model.TreeId = 0
	return model, nil
}

func NewPathEntryInfoDB(PathEntryInfo core.PathEntryInfo) (_ *PathEntryInfoDB, err error) {
	model := &PathEntryInfoDB{
		PathEntryInfo: PathEntryInfo,
	}
	model.Key = PublicKeyContainer{
		_publicKey: PathEntryInfo.Key,
	}
	publicKey := model.Key.GetPKIXPublicKey()
	model.Key.MarshalPKIXPublicKey(&publicKey)

	model.Path = Blob{
		_uint: PathEntryInfo.Path,
	}
	model.Path.ParseUint64Sliсe(model.Path._uint)
	return model, nil
}

func NewSessionInfoDB(SessionInfo core.SessionInfo) (_ *SessionInfoDB, err error) {
	model := &SessionInfoDB{
		SessionInfo: SessionInfo,
	}
	model.Key = PublicKeyContainer{
		_publicKey: SessionInfo.Key,
	}
	publicKey := model.Key.GetPKIXPublicKey()
	model.Key.MarshalPKIXPublicKey(&publicKey)
	return model, nil
}

type Blob struct {
	_byte []byte
	_uint []uint64
}

type PublicKeyContainer struct {
	_byte      []byte
	_publicKey ed25519.PublicKey
}

type ErrorContainer struct {
	_ErrStr sql.NullString
	_err    error
}

func (blob *PublicKeyContainer) GetPKIXPublicKeyBytes() []byte {
	if blob._publicKey == nil {
		return nil
	}
	result, err := MarshalPKIXPublicKey(&blob._publicKey)
	if err != nil {
		return nil
	}
	return result
}

func (blob *PublicKeyContainer) GetPKIXPublicKey() ed25519.PublicKey {
	if blob._publicKey == nil {
		return nil
	}
	return blob._publicKey
}

func (blob *PublicKeyContainer) ParsePKIXPublicKey(derBytes *[]byte) {
	result, err := ParsePKIXPublicKey(derBytes)
	if err != nil {
		return
	}
	blob._byte = *derBytes
	blob._publicKey = result
}

func (blob *PublicKeyContainer) MarshalPKIXPublicKey(PublicKey *ed25519.PublicKey) {
	result, err := MarshalPKIXPublicKey(PublicKey)
	if err != nil {
		return
	}
	blob._publicKey = *PublicKey
	blob._byte = result
}

func (blob *Blob) ConvertToUintSliсe() []uint64 {
	if blob._byte == nil {
		return nil
	}
	result, err := ConvertToUintSliсe(blob._byte)
	if err != nil {
		return nil
	}
	return result
}

func (blob *Blob) ConvertToByteSliсe() []byte {
	if blob._uint == nil {
		return nil
	}
	result := ConvertToByteSliсe(blob._uint)
	return result
}

func (blob *Blob) ParseByteSliсe(slice []byte) {
	result, err := ConvertToUintSliсe(slice)
	if err != nil {
		return
	}
	blob._byte = slice
	blob._uint = result
}

func (blob *Blob) ParseUint64Sliсe(slice []uint64) {
	blob._uint = slice
	blob._byte = ConvertToByteSliсe(slice)
}

func (ErrorContainer *ErrorContainer) ParseError(err error) {
	if err != nil {
		ErrorContainer._ErrStr = sql.NullString{
			String: err.Error(),
			Valid:  true}
		ErrorContainer._err = err
	}
}

func (ErrorContainer *ErrorContainer) ParseMessage(message string) {
	if message != "" {
		ErrorContainer._ErrStr = sql.NullString{
			String: message,
			Valid:  true}
		ErrorContainer._err = errors.New(message)
	}
}

func (ErrorContainer *ErrorContainer) GetError() error {
	if ErrorContainer._err != nil {
		return ErrorContainer._err
	}
	return nil
}

func (ErrorContainer *ErrorContainer) GetErrorMessage() string {
	if ErrorContainer._ErrStr.Valid {
		return ErrorContainer._ErrStr.String
	}
	return ""
}

func (ErrorContainer *ErrorContainer) GetErrorSqlError() sql.NullString {
	return ErrorContainer._ErrStr
}

func ConvertToByteSliсe(uintSlice []uint64) []byte {
	var ByteSlice []byte
	if uintSlice != nil {
		ByteSlice = make([]byte, len(uintSlice)*8)
		for i, coord := range uintSlice {
			binary.LittleEndian.PutUint64(ByteSlice[i*8:], coord)
		}
	}
	return ByteSlice
}

func ConvertToUintSliсe(ByteSlise []byte) (_ []uint64, err error) {
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
