package db

import (
	"database/sql"
	"os"
	"path"
)

type DbConfig struct {
	Uri    string
	DB     *sql.DB
	Name   string
	Driver string
}

var IsOpened = false

func New(driver string, schemas *[]string, uri string) (*DbConfig, error) {
	name := path.Base(uri)
	db, err := initDB(driver, schemas, uri)
	if err != nil {
		return nil, err
	}
	cfg := &DbConfig{
		DB:     db,
		Uri:    uri,
		Name:   name,
		Driver: driver,
	}
	return cfg, nil
}

func initDB(driver string, schemas *[]string, uri string) (*sql.DB, error) {
	database, err := sql.Open(driver, uri)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	tx, err := database.Begin()
	if err != nil {
		return nil, err
	}
	for _, schema := range *schemas {
		_, err := tx.Exec(schema)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	tx.Commit()
	return database, nil
}

func (cfg *DbConfig) DeleteDb() error {
	err := os.Remove(cfg.Uri)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *DbConfig) OpenDb() error {
	db, err := sql.Open(cfg.Driver, cfg.Uri)
	if err != nil {
		return err
	}
	cfg.DB = db
	IsOpened = true
	return nil
}

func (cfg *DbConfig) CloseDb() error {
	if IsOpened {
		err := cfg.DB.Close()
		if err != nil {
			return err
		}
		IsOpened = false
	}
	return nil
}

func (cfg *DbConfig) DBIsOpened() bool {
	return IsOpened
}

func (cfg *DbConfig) DBIsExist() bool {
	_, err := os.Stat(cfg.Uri)
	return !os.IsNotExist(err)
}
