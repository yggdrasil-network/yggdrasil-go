package treeentryinfodb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/db"
)

type TreeEntryInfoDBConfig struct {
	DbConfig *db.DbConfig
	name     string
}

var Name = "TreeEntryInfo"

func New() (*TreeEntryInfoDBConfig, error) {
	dir, _ := os.Getwd()
	fileName := fmt.Sprintf("%s.db", Name)
	filePath := filepath.Join(dir, fileName)
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS tree_entry_info (
		Id INTEGER NOT NULL PRIMARY KEY,
		Key BLOB,
		Parent BLOB,
		Sequence INTEGER
	);`}
	dbcfg, err := db.New("sqlite3", &schemas, filePath)
	if err != nil {
		return nil, err
	}
	cfg := &TreeEntryInfoDBConfig{
		name:     Name,
		DbConfig: dbcfg,
	}
	return cfg, nil
}

func (cfg *TreeEntryInfoDBConfig) Add(model *core.TreeEntryInfoDB) (_ sql.Result, err error) {
	query := "INSERT INTO tree_entry_info (Key, Parent, Sequence) VALUES (?, ?, ?)"
	result, err := cfg.DbConfig.DB.Exec(query,
		model.KeyBytes,
		model.ParentBytes,
		model.Sequence,
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

func (cfg *TreeEntryInfoDBConfig) Remove(model *core.TreeEntryInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec("DELETE FROM tree_entry_info WHERE Id = ?",
		model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *TreeEntryInfoDBConfig) Update(model *core.TreeEntryInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec(`UPDATE tree_entry_info 
	SET 
		Sequence = ?,
		Key = ?,
		Parent = ?
	WHERE 
		Id = ?`,
		model.Sequence, model.KeyBytes, model.ParentBytes, model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *TreeEntryInfoDBConfig) Get(model *core.TreeEntryInfoDB) (_ *sql.Rows, err error) {
	rows, err := cfg.DbConfig.DB.Query("SELECT Sequence, Key, Parent FROM tree_entry_info WHERE Id = ?",
		model.Id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&model.Sequence, &model.KeyBytes, &model.ParentBytes)
		if err != nil {
			return nil, err
		}
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
