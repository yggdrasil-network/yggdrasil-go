package sessioninfodb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	db "github.com/yggdrasil-network/yggdrasil-go/src/db/dbConfig"
)

type SessionInfoDBConfig struct {
	DbConfig *db.DbConfig
	name     string
}

var (
	Name = "SessionInfo"
	Path = ""
)

func New() (*SessionInfoDBConfig, error) {
	var path string
	if Path == "" {
		dir, _ := os.Getwd()
		fileName := fmt.Sprintf("%s.db", Name)
		path = filepath.Join(dir, fileName)
	} else {
		path = Path
	}
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS session_info (
		Id INTEGER NOT NULL PRIMARY KEY,
		Key BLOB,
		RXBytes INTEGER,
		TXBytes INTEGER,
		Duration INTEGER,
		DateTime TEXT
	);`}
	dbcfg, err := db.New("sqlite3", &schemas, path)
	if err != nil {
		return nil, err
	}
	cfg := &SessionInfoDBConfig{
		name:     Name,
		DbConfig: dbcfg,
	}
	return cfg, nil
}

func Open() (*SessionInfoDBConfig, error) {
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
	cfg := &SessionInfoDBConfig{
		name:     Name,
		DbConfig: dbcfg,
	}
	return cfg, nil
}

func (cfg *SessionInfoDBConfig) Add(model *db.SessionInfoDB) (_ sql.Result, err error) {
	query := `
			INSERT INTO 
				session_info 
				(Key, RXBytes, TXBytes, Duration, DateTime) 
			VALUES 
				(?, ?, ?, ?, datetime('now'))`
	result, err := cfg.DbConfig.DB.Exec(query,
		model.Key.GetPKIXPublicKeyBytes(),
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

func (cfg *SessionInfoDBConfig) Remove(model *db.SessionInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec("DELETE FROM session_info WHERE Id = ?",
		model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *SessionInfoDBConfig) Update(model *db.SessionInfoDB) (err error) {
	_, err = cfg.DbConfig.DB.Exec(`UPDATE session_info 
	SET 
		RXBytes = RXBytes + ?,
		TXBytes = TXBytes + ?,
		Duration = Duration + ?,
		Key = ?
	WHERE 
		Id = ?`,
		model.RXBytes, model.TXBytes, model.Uptime, model.Key.GetPKIXPublicKeyBytes(), model.Id)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *SessionInfoDBConfig) Get(model *db.SessionInfoDB) (_ *sql.Rows, err error) {
	rows, err := cfg.DbConfig.DB.Query("SELECT RXBytes, TXBytes, Duration, Key FROM session_info WHERE Id = ?",
		model.Id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var _key []byte
	for rows.Next() {
		err = rows.Scan(&model.RXBytes, &model.TXBytes, &model.Uptime, &_key)
		if err != nil {
			return nil, err
		}
		model.Key.ParsePKIXPublicKey(&_key)
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
