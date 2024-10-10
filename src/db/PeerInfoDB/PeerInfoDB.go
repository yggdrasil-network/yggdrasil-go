package peerinfodb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	db "github.com/yggdrasil-network/yggdrasil-go/src/db/dbConfig"
)

type PeerInfoDBConfig struct {
	DbConfig *db.DbConfig
	name     string
}

var (
	Name = "PeerInfo"
	Path = ""
)

func New() (*PeerInfoDBConfig, error) {
	var path string
	if Path == "" {
		dir, _ := os.Getwd()
		fileName := fmt.Sprintf("%s.db", Name)
		path = filepath.Join(dir, fileName)
	} else {
		path = Path
	}
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
		latency SMALLINT,
		DateTime TEXT
	);`}
	dbcfg, err := db.New("sqlite3", &schemas, path)
	if err != nil {
		return nil, err
	}
	cfg := &PeerInfoDBConfig{
		name:     Name,
		DbConfig: dbcfg,
	}
	return cfg, nil
}

func Open() (*PeerInfoDBConfig, error) {
	var path string
	if Path == "" {
		dir, _ := os.Getwd()
		fileName := fmt.Sprintf("%s.db", Name)
		path = filepath.Join(dir, fileName)
	} else {
		path = Path
	}
	dbcfg, err := db.OpenIfExist("sqlite3", path)
	if err != nil {
		return nil, err
	}
	cfg := &PeerInfoDBConfig{
		name:     Name,
		DbConfig: dbcfg,
	}
	return cfg, nil
}

func (cfg *PeerInfoDBConfig) Add(model *db.PeerInfoDB) (_ sql.Result, err error) {
	query := `
			INSERT OR REPLACE INTO 
				peer_infos 
				(uri, up, inbound, last_error, last_error_time, key, root, 
				coords, port, priority, Rxbytes, Txbytes, uptime, latency, DateTime) 
			VALUES 
				(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`
	result, err := cfg.DbConfig.DB.Exec(query,
		model.URI,
		model.Up,
		model.Inbound,
		model.Error.GetErrorMessage(),
		model.LastErrorTime,
		model.Key.GetPKIXPublicKeyBytes(),
		model.Root.GetPKIXPublicKeyBytes(),
		model.Coords.ConvertToByteSliсe(),
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

func (cfg *PeerInfoDBConfig) Remove(model *db.PeerInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec("DELETE FROM peer_infos WHERE Id = ?",
		model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *PeerInfoDBConfig) Get(model *db.PeerInfoDB) (_ *sql.Rows, err error) {
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
	var _key []byte
	var _root []byte
	var _err string
	var _coords []byte
	for rows.Next() {
		err = rows.Scan(&model.Up, &model.Inbound, &_err, &model.LastErrorTime, &_coords,
			&model.Port, &model.Priority, &model.RXBytes, &model.TXBytes, &model.Uptime, &model.Latency,
			&model.URI, &_key, &_root)
		if err != nil {
			return rows, err
		}
	}
	model.Coords.ParseByteSliсe(_coords)
	model.Key.ParsePKIXPublicKey(&_key)
	model.Root.ParsePKIXPublicKey(&_root)
	model.Error.ParseMessage(_err)
	return rows, nil
}

func (cfg *PeerInfoDBConfig) Update(model *db.PeerInfoDB) (err error) {
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
		model.Up, model.Inbound, model.Error.GetErrorSqlError(), model.LastErrorTime, model.Coords.ConvertToByteSliсe(), model.Port, model.Priority,
		model.RXBytes, model.TXBytes, model.Uptime, model.Latency, model.URI, model.Key.GetPKIXPublicKeyBytes(), model.Root.GetPKIXPublicKeyBytes(), model.Id)
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
