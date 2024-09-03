package peerinfodb

import (
	"database/sql"
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
		Id INTEGER NOT NULL PRIMARY KEY,
		uri TEXT,
		up INTEGER,
		inbound INTEGER,
		last_error TEXT NULL,
		last_error_time TIMESTAMP NULL,
		key BLOB,
		root BLOB,
		coords BLOB,
		port INT,
		priority TINYINT,
		Rxbytes BIGINT,
		Txbytes BIGINT,
		uptime INTEGER,
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

func (cfg *PeerInfoDBConfig) Add(model *core.PeerInfoDB) (_ sql.Result, err error) {
	query := "INSERT OR REPLACE INTO peer_infos (uri, up, inbound, last_error, last_error_time, key, root, coords, port, priority, Rxbytes, Txbytes, uptime, latency) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	result, err := cfg.DbConfig.DB.Exec(query,
		model.URI,
		model.Up,
		model.Inbound,
		model.PeerErr,
		model.LastErrorTime,
		model.KeyBytes,
		model.RootBytes,
		model.CoordsBytes,
		model.Port,
		model.Priority,
		model.RXBytes,
		model.TXBytes,
		model.Uptime,
		model.Latency)
	if err != nil {
		return nil, err
	}
	LastInsertId, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	model.Id = int(LastInsertId)
	return result, nil
}

func (cfg *PeerInfoDBConfig) Remove(model *core.PeerInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec("DELETE FROM peer_infos WHERE Id = ?",
		model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *PeerInfoDBConfig) Get(model *core.PeerInfoDB) (_ *sql.Rows, err error) {
	rows, err := cfg.DbConfig.DB.Query(`
			SELECT 
				up, inbound, last_error, last_error_time, coords, port, 
				priority, Rxbytes, Txbytes, uptime, latency, uri, key, root 
			FROM 
				peer_infos 
			WHERE Id = ?`,
		model.Id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&model.Up, &model.Inbound, &model.PeerErr, &model.LastErrorTime, &model.CoordsBytes,
			&model.Port, &model.Priority, &model.RXBytes, &model.TXBytes, &model.Uptime, &model.Latency,
			&model.URI, &model.KeyBytes, &model.RootBytes)
		if err != nil {
			return rows, err
		}
	}

	model.Coords, err = core.ConvertToUintSlise(model.CoordsBytes)
	if err != nil {
		return nil, err
	}
	publickey, err := core.ParsePKIXPublicKey(&model.KeyBytes)
	if err != nil {
		return nil, err
	}
	model.Key = publickey
	publicRoot, err := core.ParsePKIXPublicKey(&model.RootBytes)
	if err != nil {
		return nil, err
	}
	model.Root = publicRoot
	model.LastError = core.ParseError(model.PeerErr)
	return rows, nil
}

func (cfg *PeerInfoDBConfig) Update(model *core.PeerInfoDB) (err error) {
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
			uptime = uptime + ?, 
			latency = ?,
			uri = ?,
			key = ?,
			root = ?
		WHERE 
			Id = ?`,
		model.Up, model.Inbound, model.PeerErr, model.LastErrorTime, model.CoordsBytes, model.Port, model.Priority,
		model.RXBytes, model.TXBytes, model.Uptime, model.Latency, model.URI, model.KeyBytes, model.RootBytes, model.Id)
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
