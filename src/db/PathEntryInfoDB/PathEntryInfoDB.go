package pathentryinfodb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/db"
)

type PathEntryInfoDBConfig struct {
	DbConfig *db.DbConfig
	name     string
}

var Name = "PathEntryInfo"

func New() (*PathEntryInfoDBConfig, error) {
	dir, _ := os.Getwd()
	fileName := fmt.Sprintf("%s.db", Name)
	filePath := filepath.Join(dir, fileName)
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS path_entry_info (
		Id INTEGER NOT NULL PRIMARY KEY,
		Key BLOB,
		Path BLOB,
		Sequence INTEGER
	);`}
	dbcfg, err := db.New("sqlite3", &schemas, filePath)
	if err != nil {
		return nil, err
	}
	cfg := &PathEntryInfoDBConfig{
		name:     Name,
		DbConfig: dbcfg,
	}
	return cfg, nil
}

func (cfg *PathEntryInfoDBConfig) Add(model *core.PathEntryInfoDB) (_ sql.Result, err error) {
	query := "INSERT INTO path_entry_info (Key, Path, Sequence) VALUES (?, ?, ?)"
	result, err := cfg.DbConfig.DB.Exec(
		query,
		model.Key.GetPKIXPublicKeyBytes(),
		model.Path.ConvertToByteSliсe(),
		model.Sequence)
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

func (cfg *PathEntryInfoDBConfig) Remove(model *core.PathEntryInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec("DELETE FROM path_entry_info WHERE Id = ?",
		model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *PathEntryInfoDBConfig) Update(model *core.PathEntryInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec(`UPDATE path_entry_info 
	SET 
		Sequence = ?,
		Key = ?,
		Path = ?
	WHERE 
		Id = ?`,
		model.Sequence, model.Key.GetPKIXPublicKeyBytes(), model.Path.ConvertToByteSliсe(), model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *PathEntryInfoDBConfig) Get(model *core.PathEntryInfoDB) (_ *sql.Rows, err error) {
	rows, err := cfg.DbConfig.DB.Query("SELECT Sequence, Key, Path FROM path_entry_info WHERE Id = ?",
		model.Id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var _key []byte
	var _path []byte
	for rows.Next() {
		err = rows.Scan(&model.Sequence, &_key, &_path)
		if err != nil {
			return nil, err
		}
		model.Key.ParsePKIXPublicKey(&_key)
		model.Path.ParseByteSliсe(_path)
	}
	return rows, nil
}

func (cfg *PathEntryInfoDBConfig) Count() (int, error) {
	var count int
	err := cfg.DbConfig.DB.QueryRow("SELECT COUNT(*) FROM path_entry_info").Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
