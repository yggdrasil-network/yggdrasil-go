package sessioninfodb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"github.com/yggdrasil-network/yggdrasil-go/src/db"
)

type SessionInfoDBConfig struct {
	DbConfig *db.DbConfig
	name     string
}

var Name = "SessionInfo"

func New() (*SessionInfoDBConfig, error) {
	dir, _ := os.Getwd()
	fileName := fmt.Sprintf("%s.db", Name)
	filePath := filepath.Join(dir, fileName)
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS session_info (
		Id INTEGER NOT NULL PRIMARY KEY,
		Key BLOB,
		RXBytes INTEGER,
		TXBytes INTEGER,
		Duration INTEGER
	);`}
	dbcfg, err := db.New("sqlite3", &schemas, filePath)
	if err != nil {
		return nil, err
	}
	cfg := &SessionInfoDBConfig{
		name:     Name,
		DbConfig: dbcfg,
	}
	return cfg, nil
}

func (cfg *SessionInfoDBConfig) Add(model *core.SessionInfoDB) (_ sql.Result, err error) {
	query := "INSERT INTO session_info (Key, RXBytes, TXBytes, Duration) VALUES (?, ?, ?, ?)"
	result, err := cfg.DbConfig.DB.Exec(query,
		model.KeyBytes,
		model.RXBytes,
		model.TXBytes,
		model.Uptime,
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

func (cfg *SessionInfoDBConfig) Remove(model *core.SessionInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec("DELETE FROM session_info WHERE Id = ?",
		model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *SessionInfoDBConfig) Update(model *core.SessionInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec(`UPDATE session_info 
	SET 
		RXBytes = RXBytes + ?,
		TXBytes = TXBytes + ?,
		Duration = Duration + ?,
		Key = ?
	WHERE 
		Id = ?`,
		model.RXBytes, model.TXBytes, model.Uptime, model.KeyBytes, model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *SessionInfoDBConfig) Get(model *core.SessionInfoDB) (_ *sql.Rows, err error) {
	rows, err := cfg.DbConfig.DB.Query("SELECT RXBytes, TXBytes, Duration, Key FROM session_info WHERE Id = ?",
		model.Id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&model.RXBytes, &model.TXBytes, &model.Uptime, &model.KeyBytes)
		if err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func (cfg *SessionInfoDBConfig) Count() (int, error) {
	var count int
	err := cfg.DbConfig.DB.QueryRow("SELECT COUNT(*) FROM session_info").Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
