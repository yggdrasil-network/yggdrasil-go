package db_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"

	pathentryinfodb "github.com/yggdrasil-network/yggdrasil-go/src/db/PathEntryInfoDB"
	db "github.com/yggdrasil-network/yggdrasil-go/src/db/dbConfig"
)

func TestSelectPathEntryInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := pathentryinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.PathEntryInfo{
		Key:      pubkey,
		Path:     []uint64{0, 0, 0},
		Sequence: 100,
	}
	model, err := db.NewPathEntryInfoDB(entry)
	require.NoError(t, err)

	rows := sqlmock.NewRows([]string{"Sequence", "Key", "Path"}).
		AddRow(100, model.Key.GetPKIXPublicKeyBytes(), model.Path.ConvertToByteSliсe())

	mock.ExpectQuery("SELECT (.+) FROM path_entry_info WHERE Id = \\?").
		WithArgs(model.Id).
		WillReturnRows(rows)

	_, err = cfg.Get(model)
	t.Log(err)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)

}

func TestInsertPathEntryInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := pathentryinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.PathEntryInfo{
		Key:      pubkey,
		Path:     []uint64{0, 0, 0},
		Sequence: 100,
	}
	model, err := db.NewPathEntryInfoDB(entry)
	require.NoError(t, err)
	mock.ExpectExec("INSERT INTO path_entry_info").
		WithArgs(
			model.Key.GetPKIXPublicKeyBytes(),
			model.Path.ConvertToByteSliсe(),
			model.Sequence,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	_, err = cfg.Add(model)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestDeletePathEntryInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := pathentryinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.PathEntryInfo{
		Key:      pubkey,
		Path:     []uint64{0, 0, 0},
		Sequence: 100,
	}
	model, err := db.NewPathEntryInfoDB(entry)
	require.NoError(t, err)
	mock.ExpectExec("DELETE FROM path_entry_info WHERE Id = \\?").
		WithArgs(
			model.Id,
		).WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.Remove(model)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestUpdatePathEntryInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := pathentryinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.PathEntryInfo{
		Key:      pubkey,
		Path:     []uint64{0, 0, 0},
		Sequence: 100,
	}
	model, err := db.NewPathEntryInfoDB(entry)
	require.NoError(t, err)
	mock.ExpectExec(`
		UPDATE path_entry_info 
		SET 
			Sequence = \?,
			Key = \?,
			Path = \?
		WHERE
			Id = \?`).
		WithArgs(
			model.Sequence,
			model.Key.GetPKIXPublicKeyBytes(),
			model.Path.ConvertToByteSliсe(),
			model.Id,
		).WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.Update(model)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestMainPathEntryInfo(t *testing.T) {
	pathentryinfodb.Name = fmt.Sprintf(
		"%s.%s",
		pathentryinfodb.Name,
		strconv.Itoa(int(time.Now().Unix())),
	)
	pathentryinfodb, err := pathentryinfodb.New()
	require.NoError(t, err)

	pathentryinfodb.DbConfig.OpenDb()
	isOpened := pathentryinfodb.DbConfig.DBIsOpened()
	condition := func() bool {
		return isOpened
	}
	require.Condition(t, condition, "Expected db is opened", isOpened)

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	secondPubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.PathEntryInfo{
		Key:      pubkey,
		Path:     []uint64{0, 0, 0},
		Sequence: 100,
	}
	model, err := db.NewPathEntryInfoDB(entry)
	require.NoError(t, err)

	secondEntry := core.PathEntryInfo{
		Key:      secondPubKey,
		Path:     []uint64{0, 0, 0},
		Sequence: 100,
	}
	secondModel, err := db.NewPathEntryInfoDB(secondEntry)
	require.NoError(t, err)

	_, err = pathentryinfodb.Add(model)
	require.NoError(t, err)
	_, err = pathentryinfodb.Add(secondModel)
	require.NoError(t, err)
	count, err := pathentryinfodb.Count()
	require.NoError(t, err)
	condition = func() bool {
		return count == 2
	}
	require.Condition(t, condition, "Expected count to be 2", count)

	err = pathentryinfodb.Remove(secondModel)
	require.NoError(t, err)
	count, err = pathentryinfodb.Count()
	require.NoError(t, err)

	condition = func() bool {
		return count == 1
	}
	require.Condition(t, condition, "Expected count to be 1", count)

	model.Sequence = 10
	err = pathentryinfodb.Update(model)
	require.NoError(t, err)
	_, err = pathentryinfodb.Get(model)
	require.NoError(t, err)

	err = pathentryinfodb.Update(secondModel)
	require.NoError(t, err)

	condition = func() bool {
		return model.Sequence == 10
	}
	require.Condition(t, condition, "Inner exception")

	pathentryinfodb.Remove(model)
	count, err = pathentryinfodb.Count()
	require.NoError(t, err)

	condition = func() bool {
		return count == 0
	}
	require.Condition(t, condition, "Expected count to be 0", count)

	err = pathentryinfodb.DbConfig.CloseDb()
	isOpened = pathentryinfodb.DbConfig.DBIsOpened()
	require.NoError(t, err)

	condition = func() bool {
		return !isOpened
	}
	require.Condition(t, condition, "Expected db is not opened", isOpened)

	err = pathentryinfodb.DbConfig.DeleteDb()
	require.NoError(t, err)

	isExist := pathentryinfodb.DbConfig.DBIsExist()
	condition = func() bool {
		return !isExist
	}
	require.Condition(t, condition, "Expected db is not exist", isExist)
}
