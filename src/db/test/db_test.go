package db_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
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
	peerinfo := core.PeerInfo{
		Coords: []uint64{1, 2, 3, 4},
	}
	peer, err := core.NewPeerInfoDB(peerinfo)
	require.NoError(t, err)

	target := []byte{1, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0}
	coordinates := peer.Coords.ConvertToByteSliсe()
	if !reflect.DeepEqual(target, coordinates) {
		t.Error(fmt.Errorf("Not equal"))
	}
}

func TestPeerSetCoords(t *testing.T) {
	peerinfo := core.PeerInfo{}
	peer, err := core.NewPeerInfoDB(peerinfo)
	require.NoError(t, err)
	peer.Coords.ParseByteSliсe([]byte{4, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0})
	require.NoError(t, err)
	coords := []uint64{4, 3, 2, 1}
	if !reflect.DeepEqual(coords, peer.Coords.ConvertToUintSliсe()) {
		t.Error(fmt.Errorf("Not equal"))
	}
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
	peerinfo := core.PeerInfo{
		URI:           "test.test",
		Up:            true,
		Inbound:       true,
		LastError:     errors.New("test"),
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
	peer, err := core.NewPeerInfoDB(peerinfo)
	require.NoError(t, err)

	mock.ExpectExec("INSERT OR REPLACE INTO peer_infos").
		WithArgs(
			peer.URI,
			peer.Up,
			peer.Inbound,
			peer.Error.GetErrorMessage(),
			peer.LastErrorTime,
			peer.Key.GetPKIXPublicKeyBytes(),
			peer.Root.GetPKIXPublicKeyBytes(),
			peer.Coords.ConvertToByteSliсe(),
			peer.Port,
			peer.Priority,
			peer.RXBytes,
			peer.TXBytes,
			peer.Uptime,
			peer.Latency,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	_, err = cfg.Add(peer)
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

	peerinfo := core.PeerInfo{
		URI:           "test.test",
		Up:            true,
		Inbound:       true,
		LastError:     errors.New("test"),
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
	peer, err := core.NewPeerInfoDB(peerinfo)
	require.NoError(t, err)

	mock.ExpectExec("DELETE FROM peer_infos WHERE Id = \\?").
		WithArgs(peer.Id).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.Remove(peer)
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

	peerinfo := core.PeerInfo{
		URI:           "test.test",
		Up:            true,
		Inbound:       true,
		LastError:     errors.New("test"),
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
	peer, err := core.NewPeerInfoDB(peerinfo)
	require.NoError(t, err)

	rows := sqlmock.NewRows([]string{"up", "inbound", "last_error", "last_error_time", "coords",
		"port", "priority", "Rxbytes", "Txbytes", "uptime", "latency", "uri", "key", "root"}).
		AddRow(peer.Up, peer.Inbound, peer.Error.GetErrorMessage(), peer.LastErrorTime, peer.Coords.ConvertToByteSliсe(),
			peer.Port, peer.Priority, peer.RXBytes, peer.TXBytes, peer.Uptime, peer.Latency,
			peer.URI, peer.Key.GetPKIXPublicKeyBytes(), peer.Root.GetPKIXPublicKeyBytes())

	mock.ExpectQuery("SELECT (.+) FROM peer_infos WHERE Id = \\?").
		WithArgs(peer.Id).
		WillReturnRows(rows)

	_, err = cfg.Get(peer)
	t.Log(err)
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
	peerinfo := core.PeerInfo{
		URI:           "test.test",
		Up:            true,
		Inbound:       true,
		LastError:     errors.New("test"),
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
	peer, err := core.NewPeerInfoDB(peerinfo)
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
			uptime = uptime \+ \?,
			latency = \?,
			uri = \?,
			key = \?,
			root = \?
		WHERE 
			Id = \?`).
		WithArgs(
			peer.Up, peer.Inbound, peer.Error.GetErrorMessage(), peer.LastErrorTime, peer.Coords.ConvertToByteSliсe(), peer.Port, peer.Priority,
			peer.RXBytes, peer.TXBytes, peer.Uptime, peer.Latency, peer.URI, peer.Key.GetPKIXPublicKeyBytes(), peer.Root.GetPKIXPublicKeyBytes(), peer.Id).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.Update(peer)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

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
	peerinfo := core.PeerInfo{
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
	peer, err := core.NewPeerInfoDB(peerinfo)
	require.NoError(t, err)
	root2PubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	peerinfo2 := core.PeerInfo{
		URI:           "new.test",
		Up:            true,
		Inbound:       true,
		LastError:     errors.New("test2"),
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
	peer2, err := core.NewPeerInfoDB(peerinfo2)
	require.NoError(t, err)
	_, err = peerdb.Add(peer)
	require.NoError(t, err)
	_, err = peerdb.Add(peer2)
	require.NoError(t, err)
	count, err := peerdb.Count()
	require.NoError(t, err)
	condition = func() bool {
		return count == 2
	}
	require.Condition(t, condition, "Expected count to be 2", count)

	err = peerdb.Remove(peer)
	require.NoError(t, err)
	count, err = peerdb.Count()
	require.NoError(t, err)
	condition = func() bool {
		return count == 1
	}

	require.Condition(t, condition, "Expected count to be 1", count)

	peer2.URI = "test"
	peer2.Up = false
	peer2.Inbound = false
	peer2.Error.ParseMessage("Test")
	peer2.LastErrorTime = sql.NullTime{}
	peer2.Key.MarshalPKIXPublicKey(&root2PubKey)
	peer2.Root.MarshalPKIXPublicKey(&pubkey)
	peer2.Coords.ParseUint64Sliсe([]uint64{1, 1, 1, 1})
	peer2.Port = 80
	peer2.Priority = 2
	peer2.RXBytes = 1024
	peer2.TXBytes = 1024
	peer2.Uptime = 1000
	peer2.Latency = 10
	err = peerdb.Update(peer2)
	require.NoError(t, err)
	_, err = peerdb.Get(peer2)
	require.NoError(t, err)

	condition = func() bool {
		return peer2.URI == "test" &&
			peer2.Up == false &&
			peer2.Inbound == false &&
			peer2.Error.GetErrorMessage() == "Test" &&
			peer2.LastErrorTime.Time.IsZero() &&
			peer2.Key.GetPKIXPublicKey().Equal(root2PubKey) &&
			peer2.Root.GetPKIXPublicKey().Equal(pubkey) &&
			reflect.DeepEqual(peer2.Coords.ConvertToUintSliсe(), []uint64{1, 1, 1, 1}) &&
			peer2.Port == 80 &&
			peer2.Priority == 2 &&
			peer2.RXBytes == 2048 &&
			peer2.TXBytes == 3072 &&
			peer2.Uptime == 4600 &&
			peer2.Latency == 10
	}

	require.Condition(t, condition, "Inner exception")

	peerdb.Remove(peer2)
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
