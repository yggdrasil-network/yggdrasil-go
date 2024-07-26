package peerinfodb

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/db"
)

type PeerInfoDBConfig struct {
	DbConfig *db.DbConfig
	name     string
}

var Name = "PeerInfo"

func New() (*PeerInfoDBConfig, error) {
	dir, _ := os.Getwd()
	fileName := fmt.Sprintf("%s.db", Name)
	filePath := filepath.Join(dir, fileName)
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS peer_infos (
		uri TEXT,
		up BOOLEAN,
		inbound BOOLEAN,
		last_error VARCHAR,
		last_error_time TIMESTAMP,
		key VARCHAR,
		root VARCHAR,
		coords VARCHAR,
		port INT,
		priority TINYINT,
		Rxbytes BIGINT,
		Txbytes BIGINT,
		uptime BIGINT,
		latency SMALLINT
	);`}
	dbcfg, err := db.New("sqlite3", &schemas, filePath)
	if err != nil {
		return nil, err
	}
	cfg := &PeerInfoDBConfig{
		name:     Name,
		DbConfig: dbcfg,
	}
	return cfg, nil
}

func (cfg *PeerInfoDBConfig) AddPeer(peer core.PeerInfo) (err error) {
	var key, root []byte
	if peer.Key != nil {
		key, err = x509.MarshalPKIXPublicKey(peer.Key)
		if err != nil {
			return err
		}
	}
	if peer.Root != nil {
		root, err = x509.MarshalPKIXPublicKey(peer.Root)
		if err != nil {
			return err
		}
	}
	var peerErr interface{}
	if peer.LastError != nil {
		peerErr = peer.LastError.Error()
	} else {
		peerErr = nil
	}
	var coordsBlob []byte
	if peer.Coords != nil {
		coordsBlob = make([]byte, len(peer.Coords)*8)
		for i, coord := range peer.Coords {
			binary.LittleEndian.PutUint64(coordsBlob[i*8:], coord)
		}
	}
	if !cfg.DbConfig.DBIsOpened() {
		return nil
	}
	_, err = cfg.DbConfig.DB.Exec(`
    INSERT OR REPLACE INTO peer_infos 
    (
		uri,
		up,
		inbound,
		last_error,
		last_error_time,
		key,
		root,
		coords,
		port,
		priority,
		Rxbytes,
		Txbytes,
		uptime,
		latency
	) 
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		peer.URI, peer.Up, peer.Inbound, peerErr, peer.LastErrorTime, key, root, coordsBlob, peer.Port, peer.Priority, peer.RXBytes, peer.TXBytes, peer.Uptime, peer.Latency)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *PeerInfoDBConfig) RemovePeer(peer core.PeerInfo) (err error) {
	key, err := x509.MarshalPKIXPublicKey(peer.Key)
	if err != nil {
		return err
	}
	root, err := x509.MarshalPKIXPublicKey(peer.Root)
	if err != nil {
		return err
	}
	_, err = cfg.DbConfig.DB.Exec("DELETE FROM peer_infos WHERE uri = ? AND key = ? AND root = ?",
		peer.URI, key, root)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *PeerInfoDBConfig) GetPeer(peer *core.PeerInfo) (err error) {
	key, err := x509.MarshalPKIXPublicKey(peer.Key)
	if err != nil {
		return err
	}
	root, err := x509.MarshalPKIXPublicKey(peer.Root)
	if err != nil {
		return err
	}
	row := cfg.DbConfig.DB.QueryRow("SELECT * FROM peer_infos WHERE uri = ? AND key = ? AND root = ?",
		peer.URI, key, root)
	var coord []byte
	var peerErr interface{}
	err = row.Scan(&peer.URI, &peer.Up, &peer.Inbound, &peerErr, &peer.LastErrorTime, &key, &root, &coord, &peer.Port, &peer.Priority, &peer.RXBytes, &peer.TXBytes, &peer.Uptime, &peer.Latency)
	if err != nil {
		return err
	}

	parsedKey, err := x509.ParsePKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	ParsedRoot, err := x509.ParsePKCS8PrivateKey(root)
	if err != nil {
		return err
	}
	peer.Key = parsedKey.(ed25519.PublicKey)
	peer.Root = ParsedRoot.(ed25519.PublicKey)
	return nil
}

func (cfg *PeerInfoDBConfig) UpdatePeer(peer core.PeerInfo) (err error) {
	key, err := x509.MarshalPKIXPublicKey(peer.Key)
	if err != nil {
		return err
	}
	root, err := x509.MarshalPKIXPublicKey(peer.Root)
	if err != nil {
		return err
	}
	var peerErr interface{}
	if peer.LastError != nil {
		peerErr = peer.LastError.Error()
	} else {
		peerErr = nil
	}
	var coordsBlob []byte
	if peer.Coords != nil {
		coordsBlob = make([]byte, len(peer.Coords)*8)
		for i, coord := range peer.Coords {
			binary.LittleEndian.PutUint64(coordsBlob[i*8:], coord)
		}
	}
	_, err = cfg.DbConfig.DB.Exec(`UPDATE peer_infos 
		SET 
			up = ?, 
			inbound = ?, 
			last_error = ?, 
			last_error_time = ?, 
			coords = ?, 
			port = ?, 
			priority = ?, 
			RXBytes = RXBytes + ?, 
			TXBytes = TXBytes + ?, 
			uptime = ?, 
			latency = ? 
		WHERE 
			uri = ? AND key = ? AND root = ?`,
		peer.Up, peer.Inbound, peerErr, peer.LastErrorTime, coordsBlob, peer.Port, peer.Priority,
		peer.RXBytes, peer.TXBytes, peer.Uptime, peer.Latency, peer.URI, key, root)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *PeerInfoDBConfig) Count() (int, error) {
	var count int
	err := cfg.DbConfig.DB.QueryRow("SELECT COUNT(*) FROM peer_infos").Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
