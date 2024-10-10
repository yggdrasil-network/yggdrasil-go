package treeentryinfodb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	db "github.com/yggdrasil-network/yggdrasil-go/src/db/dbConfig"
)

type TreeEntryInfoDBConfig struct {
	DbConfig *db.DbConfig
	name     string
}

var (
	Name = "TreeEntryInfo"
	Path = ""
)

func New() (*TreeEntryInfoDBConfig, error) {
	var path string
	if Path == "" {
		dir, _ := os.Getwd()
		fileName := fmt.Sprintf("%s.db", Name)
		path = filepath.Join(dir, fileName)
	} else {
		path = Path
	}
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS tree_entry_info (
		Id INTEGER NOT NULL PRIMARY KEY,
		Key BLOB,
		Parent BLOB,
		Sequence INTEGER,
		TreeId  INTEGER NULL,
		DateTime TEXT
	);`}
	dbcfg, err := db.New("sqlite3", &schemas, path)
	if err != nil {
		return nil, err
	}
	cfg := &TreeEntryInfoDBConfig{
		name:     Name,
		DbConfig: dbcfg,
	}
	return cfg, nil
}

func Open() (*TreeEntryInfoDBConfig, error) {
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
	cfg := &TreeEntryInfoDBConfig{
		name:     Name,
		DbConfig: dbcfg,
	}
	return cfg, nil
}

func (cfg *TreeEntryInfoDBConfig) Add(model *db.TreeEntryInfoDB) (_ sql.Result, err error) {
	query := `
			INSERT INTO 
				tree_entry_info 
				(Key, Parent, Sequence, TreeId, DateTime) 
			VALUES 
				(?, ?, ?, ?, datetime('now'))`
	/*var _id sql.NullInt32 = sql.NullInt32{}
	if model.TreeId != 0 {
		_id = sql.NullInt32{Int32: int32(model.TreeId), Valid: true}
	}*/
	result, err := cfg.DbConfig.DB.Exec(query,
		model.Key.GetPKIXPublicKeyBytes(),
		model.Parent.GetPKIXPublicKeyBytes(),
		model.Sequence,
		model.TreeId,
	)
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

func (cfg *TreeEntryInfoDBConfig) Remove(model *db.TreeEntryInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec("DELETE FROM tree_entry_info WHERE Id = ?",
		model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *TreeEntryInfoDBConfig) Update(model *db.TreeEntryInfoDB) (err error) {
	/*var _id sql.NullInt32 = sql.NullInt32{}
	if model.TreeId != 0 {
		_id = sql.NullInt32{Int32: int32(model.TreeId), Valid: true}
	}*/
	_, err = cfg.DbConfig.DB.Exec(`UPDATE tree_entry_info 
	SET 
		Sequence = ?,
		Key = ?,
		Parent = ?,
		TreeId = ?
	WHERE 
		Id = ?`,
		model.Sequence, model.Key.GetPKIXPublicKeyBytes(), model.Parent.GetPKIXPublicKeyBytes(), model.TreeId, model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *TreeEntryInfoDBConfig) Get(model *db.TreeEntryInfoDB) (_ *sql.Rows, err error) {
	rows, err := cfg.DbConfig.DB.Query("SELECT Sequence, Key, Parent, TreeId FROM tree_entry_info WHERE Id = ?",
		model.Id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var _key []byte
	var _path []byte
	var _id sql.NullInt32
	for rows.Next() {
		err = rows.Scan(&model.Sequence, &_key, &_path, &_id)
		if err != nil {
			return nil, err
		}
		if _id.Valid {
			model.TreeId = int(_id.Int32)
		} else {
			model.TreeId = 0
		}
		model.Key.ParsePKIXPublicKey(&_key)
		model.Parent.ParsePKIXPublicKey(&_path)
	}
	return rows, nil
}

func (cfg *TreeEntryInfoDBConfig) Count() (int, error) {
	var count int
	err := cfg.DbConfig.DB.QueryRow("SELECT COUNT(*) FROM tree_entry_info").Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
