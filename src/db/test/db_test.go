package db_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	peerinfodb "github.com/yggdrasil-network/yggdrasil-go/src/db/PeerInfoDB"
)

func TestPeerGetCoords(t *testing.T) {
	peer := core.PeerInfo{
		Coords: []uint64{1, 2, 3, 4},
	}
	target := []byte{1, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0}
	var coordinates = peer.GetCoordinates()
	if reflect.DeepEqual(target, coordinates) {
		t.Error(fmt.Errorf("Not equal"))
	}
}

func TestPeerSetCoords(t *testing.T) {
	peer := core.PeerInfo{
		Coords: []uint64{1, 2, 3, 4},
	}
	target := []byte{4, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0}
	var coordinates = peer.SetCoordinates(&target)
	if reflect.DeepEqual(target, coordinates) {
		t.Error(fmt.Errorf("Not equal"))
	}
	fmt.Print(peer.Coords)
}

func TestAddPeer(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := peerinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	rootPubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	peer := core.PeerInfo{
		URI:           "test.test",
		Up:            true,
		Inbound:       true,
		LastError:     nil,
		LastErrorTime: time.Now(),
		Key:           pubkey,
		Root:          rootPubKey,
		Coords:        []uint64{0, 0, 0, 0},
		Port:          8080,
		Priority:      1,
		RXBytes:       1024,
		TXBytes:       2048,
		Uptime:        3600,
		Latency:       50.0,
	}

	pKey, err := x509.MarshalPKIXPublicKey(peer.Key)
	require.NoError(t, err)
	pKeyRoot, err := x509.MarshalPKIXPublicKey(peer.Root)
	require.NoError(t, err)
	var coordsBlob []byte
	if peer.Coords != nil {
		coordsBlob = make([]byte, len(peer.Coords)*8)
		for i, coord := range peer.Coords {
			binary.LittleEndian.PutUint64(coordsBlob[i*8:], coord)
		}
	}
	mock.ExpectExec("INSERT OR REPLACE INTO peer_infos").
		WithArgs(
			peer.URI,
			peer.Up,
			peer.Inbound,
			nil,
			peer.LastErrorTime,
			pKey,
			pKeyRoot,
			coordsBlob,
			peer.Port,
			peer.Priority,
			peer.RXBytes,
			peer.TXBytes,
			peer.Uptime,
			peer.Latency,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.AddPeer(peer)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestRemovePeer(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := peerinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	rootPubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	peer := core.PeerInfo{
		URI:           "test.test",
		Up:            true,
		Inbound:       true,
		LastError:     nil,
		LastErrorTime: time.Now(),
		Key:           pubkey,
		Root:          rootPubKey,
		Coords:        []uint64{0, 0, 0, 0},
		Port:          8080,
		Priority:      1,
		RXBytes:       1024,
		TXBytes:       2048,
		Uptime:        3600,
		Latency:       50.0,
	}

	pKey, err := x509.MarshalPKIXPublicKey(peer.Key)
	require.NoError(t, err)
	pKeyRoot, err := x509.MarshalPKIXPublicKey(peer.Root)
	require.NoError(t, err)
	mock.ExpectExec("DELETE FROM peer_infos WHERE uri = \\? AND key = \\? AND root = \\?").
		WithArgs(peer.URI, pKey, pKeyRoot).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.RemovePeer(peer)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestGetPeer(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := peerinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	rootPubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	peer := core.PeerInfo{
		URI:           "test.test",
		Up:            true,
		Inbound:       true,
		LastError:     nil,
		LastErrorTime: time.Now(),
		Key:           pubkey,
		Root:          rootPubKey,
		Coords:        []uint64{0, 0, 0, 0},
		Port:          8080,
		Priority:      1,
		RXBytes:       1024,
		TXBytes:       2048,
		Uptime:        3600,
		Latency:       50.0,
	}

	pKey, err := x509.MarshalPKIXPublicKey(peer.Key)
	require.NoError(t, err)
	pKeyRoot, err := x509.MarshalPKIXPublicKey(peer.Root)
	require.NoError(t, err)
	var coords []byte
	rows := sqlmock.NewRows([]string{"uri", "up", "Inbound", "LastError", "LastErrorTime", "Key", "Root", "Coords", "Port", "Priority", "Rxbytes", "Txbytes", "Uptime", "Latency"}).
		AddRow(peer.URI, peer.Up, peer.Inbound, peer.LastError, peer.LastErrorTime, peer.Key, peer.Root, coords, peer.Port, peer.Priority, peer.RXBytes, peer.TXBytes, peer.Uptime, peer.Latency)

	mock.ExpectQuery("SELECT * FROM peer_infos WHERE uri = ? AND key = ? AND root = ?").
		WithArgs(peer.URI, pKey, pKeyRoot).
		WillReturnRows(rows)

	err = cfg.GetPeer(&peer)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestUpdatePeer(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := peerinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	rootPubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	peer := core.PeerInfo{
		URI:           "test.test",
		Up:            true,
		Inbound:       true,
		LastError:     nil,
		LastErrorTime: time.Now(),
		Key:           pubkey,
		Root:          rootPubKey,
		Coords:        []uint64{0, 0, 0, 0},
		Port:          8080,
		Priority:      1,
		RXBytes:       1024,
		TXBytes:       2048,
		Uptime:        3600,
		Latency:       50.0,
	}

	pKey, err := x509.MarshalPKIXPublicKey(peer.Key)
	require.NoError(t, err)
	pKeyRoot, err := x509.MarshalPKIXPublicKey(peer.Root)
	var coordsBlob []byte
	if peer.Coords != nil {
		coordsBlob = make([]byte, len(peer.Coords)*8)
		for i, coord := range peer.Coords {
			binary.LittleEndian.PutUint64(coordsBlob[i*8:], coord)
		}
	}
	require.NoError(t, err)
	mock.ExpectExec(`UPDATE peer_infos 
		SET 
			up = \?, 
			inbound = \?, 
			last_error = \?, 
			last_error_time = \?, 
			coords = \?, 
			port = \?, 
			priority = \?, 
			RXBytes = RXBytes \+ \?, 
			TXBytes = TXBytes \+ \?, 
			uptime = \?, 
			latency = \? 
		WHERE 
			uri = \? AND key = \? AND root = \?`).
		WithArgs(
			peer.Up, peer.Inbound, peer.LastError, peer.LastErrorTime, coordsBlob, peer.Port, peer.Priority,
			peer.RXBytes, peer.TXBytes, peer.Uptime, peer.Latency, peer.URI, pKey, pKeyRoot).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.UpdatePeer(peer)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

// One more test here
func TestMain(t *testing.T) {
	peerinfodb.Name = fmt.Sprintf(
		"%s.%s",
		peerinfodb.Name,
		strconv.Itoa(int(time.Now().Unix())),
	)

	peerdb, err := peerinfodb.New()
	require.NoError(t, err)

	peerdb.DbConfig.OpenDb()
	isOpened := peerdb.DbConfig.DBIsOpened()
	condition := func() bool {
		return isOpened
	}
	require.Condition(t, condition, "Expected db is opened", isOpened)

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	rootPubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	peer := core.PeerInfo{
		URI:           "test.test",
		Up:            true,
		Inbound:       true,
		LastError:     nil,
		LastErrorTime: time.Now(),
		Key:           pubkey,
		Root:          rootPubKey,
		Coords:        []uint64{0, 0, 0, 0},
		Port:          8080,
		Priority:      1,
		RXBytes:       1024,
		TXBytes:       2048,
		Uptime:        3600,
		Latency:       50.0,
	}

	root2PubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	peer2 := core.PeerInfo{
		URI:           "new.test",
		Up:            true,
		Inbound:       true,
		LastError:     nil,
		LastErrorTime: time.Now(),
		Key:           pubkey,
		Root:          root2PubKey,
		Coords:        []uint64{0, 0, 0, 0},
		Port:          8080,
		Priority:      1,
		RXBytes:       1024,
		TXBytes:       2048,
		Uptime:        3600,
		Latency:       50.0,
	}

	peerdb.AddPeer(peer)
	peerdb.AddPeer(peer2)
	count, err := peerdb.Count()
	require.NoError(t, err)
	condition = func() bool {
		return count == 2
	}
	require.Condition(t, condition, "Expected count to be 2", count)

	peerdb.RemovePeer(peer)
	count, err = peerdb.Count()
	require.NoError(t, err)
	condition = func() bool {
		return count == 1
	}
	require.Condition(t, condition, "Expected count to be 1", count)

	peer2.Latency = 10
	peer2.RXBytes = 1024
	peer2.TXBytes = 1024
	peer2.Port = 80
	peerdb.UpdatePeer(peer2)
	peerdb.GetPeer(&peer2)
	condition = func() bool {
		return peer2.Latency == 10 &&
			peer2.RXBytes == 2048 &&
			peer2.TXBytes == 3072 &&
			peer2.Port == 80 && peer2.URI == "new.test" && bytes.Equal(peer.Key, pubkey)
	}
	require.Condition(t, condition, "Inner exception")

	peerdb.RemovePeer(peer2)
	count, err = peerdb.Count()
	require.NoError(t, err)

	condition = func() bool {
		return count == 0
	}

	require.Condition(t, condition, "Expected count to be 0", count)

	err = peerdb.DbConfig.CloseDb()
	isOpened = peerdb.DbConfig.DBIsOpened()

	condition = func() bool {
		return !isOpened
	}

	require.Condition(t, condition, "Expected db is not opened", isOpened)

	require.NoError(t, err)
	err = peerdb.DbConfig.DeleteDb()
	require.NoError(t, err)
	isExist := peerdb.DbConfig.DBIsExist()

	condition = func() bool {
		return !isExist
	}

	require.Condition(t, condition, "Expected db is not exist", isExist)
}
