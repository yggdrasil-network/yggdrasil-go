package selfinfodb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	db "github.com/yggdrasil-network/yggdrasil-go/src/db/dbConfig"
)

type SelfInfoDBConfig struct {
	DbConfig *db.DbConfig
	name     string
}

var (
	Name = "SelfInfo"
	Path = ""
)

func New() (*SelfInfoDBConfig, error) {
	var path string
	if Path == "" {
		dir, _ := os.Getwd()
		fileName := fmt.Sprintf("%s.db", Name)
		path = filepath.Join(dir, fileName)
	} else {
		path = Path
	}
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS self_info (
			Id INTEGER NOT NULL PRIMARY KEY,
			Key BLOB,
			RoutingEntries INTEGER,
			DateTime TEXT
		);`}
	dbcfg, err := db.New("sqlite3", &schemas, path)
	if err != nil {
		return nil, err
	}
	cfg := &SelfInfoDBConfig{
		name:     Name,
		DbConfig: dbcfg,
	}
	return cfg, nil
}

func Open() (*SelfInfoDBConfig, error) {
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
	cfg := &SelfInfoDBConfig{
		name:     Name,
		DbConfig: dbcfg,
	}
	return cfg, nil
}

func (cfg *SelfInfoDBConfig) Add(model *db.SelfInfoDB) (_ sql.Result, err error) {
	query := `
			INSERT OR REPLACE INTO 
				self_info 
				(Key, RoutingEntries, DateTime) 
			VALUES 
				(?, ?, datetime('now'))`
	result, err := cfg.DbConfig.DB.Exec(query,
		model.Key.GetPKIXPublicKeyBytes(),
		model.RoutingEntries)
	if err != nil {
		return nil, err
	}
	lastInsertId, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	model.Id = int(lastInsertId)
	return result, nil
}

func (cfg *SelfInfoDBConfig) Update(model *db.SelfInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec(`UPDATE self_info 
	SET 
		RoutingEntries = ?,
		Key = ?
	WHERE 
		Id = ?`,
		model.RoutingEntries, model.Key.GetPKIXPublicKeyBytes(), model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *SelfInfoDBConfig) Remove(model *db.SelfInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec("DELETE FROM self_info WHERE Id = ?",
		model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *SelfInfoDBConfig) Get(model *db.SelfInfoDB) (_ *sql.Rows, err error) {
	rows, err := cfg.DbConfig.DB.Query("SELECT RoutingEntries, Key FROM self_info WHERE Id = ?",
		model.Id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var _key []byte
	for rows.Next() {
		err = rows.Scan(&model.RoutingEntries, &_key)
		if err != nil {
			return rows, err
		}
		model.Key.ParsePKIXPublicKey(&_key)
	}
	return rows, nil
}

func (cfg *SelfInfoDBConfig) Count() (int, error) {
	var count int
	err := cfg.DbConfig.DB.QueryRow("SELECT COUNT(*) FROM self_info").Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
