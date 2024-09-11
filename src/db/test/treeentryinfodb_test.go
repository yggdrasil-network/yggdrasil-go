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

	treeentryinfodb "github.com/yggdrasil-network/yggdrasil-go/src/db/TreeEntryInfoDB"
)

func TestSelectTreeEntryInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := treeentryinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.TreeEntryInfo{
		Key:      pubkey,
		Parent:   pubkey,
		Sequence: 10,
	}
	model, err := core.NewTreeEntryInfoDB(entry)
	require.NoError(t, err)

	rows := sqlmock.NewRows([]string{"Sequence", "Key", "Parent"}).
		AddRow(100, model.Key.GetPKIXPublicKeyBytes(), model.Parent.GetPKIXPublicKeyBytes())

	mock.ExpectQuery("SELECT (.+) FROM tree_entry_info WHERE Id = \\?").
		WithArgs(model.Id).
		WillReturnRows(rows)

	_, err = cfg.Get(model)
	t.Log(err)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)

}

func TestInsertTreeEntryInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := treeentryinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.TreeEntryInfo{
		Key:      pubkey,
		Parent:   pubkey,
		Sequence: 10,
	}
	model, err := core.NewTreeEntryInfoDB(entry)
	require.NoError(t, err)

	mock.ExpectExec("INSERT INTO tree_entry_info").
		WithArgs(
			model.Key.GetPKIXPublicKeyBytes(),
			model.Parent.GetPKIXPublicKeyBytes(),
			model.Sequence,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	_, err = cfg.Add(model)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestDeleteTreeEntryInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := treeentryinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.TreeEntryInfo{
		Key:      pubkey,
		Parent:   pubkey,
		Sequence: 10,
	}
	model, err := core.NewTreeEntryInfoDB(entry)
	require.NoError(t, err)
	mock.ExpectExec("DELETE FROM tree_entry_info WHERE Id = \\?").
		WithArgs(
			model.Id,
		).WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.Remove(model)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestUpdateTreeEntryInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := treeentryinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.TreeEntryInfo{
		Key:      pubkey,
		Parent:   pubkey,
		Sequence: 10,
	}
	model, err := core.NewTreeEntryInfoDB(entry)
	require.NoError(t, err)
	mock.ExpectExec(`
		UPDATE tree_entry_info 
		SET 
			Sequence = \?,
			Key = \?,
			Parent = \?
		WHERE 
			Id = \?`).
		WithArgs(
			model.Sequence,
			model.Key.GetPKIXPublicKeyBytes(),
			model.Parent.GetPKIXPublicKeyBytes(),
			model.Id,
		).WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.Update(model)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestMainTreeEntryInfo(t *testing.T) {
	treeentryinfodb.Name = fmt.Sprintf(
		"%s.%s",
		treeentryinfodb.Name,
		strconv.Itoa(int(time.Now().Unix())),
	)
	treeentryinfodb, err := treeentryinfodb.New()
	require.NoError(t, err)

	treeentryinfodb.DbConfig.OpenDb()
	isOpened := treeentryinfodb.DbConfig.DBIsOpened()
	condition := func() bool {
		return isOpened
	}
	require.Condition(t, condition, "Expected db is opened", isOpened)

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	secondPubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.TreeEntryInfo{
		Key:      pubkey,
		Parent:   pubkey,
		Sequence: 10,
	}
	model, err := core.NewTreeEntryInfoDB(entry)
	require.NoError(t, err)

	secondEntry := core.TreeEntryInfo{
		Key:      secondPubKey,
		Parent:   secondPubKey,
		Sequence: 20,
	}
	secondModel, err := core.NewTreeEntryInfoDB(secondEntry)
	require.NoError(t, err)

	_, err = treeentryinfodb.Add(model)
	require.NoError(t, err)
	_, err = treeentryinfodb.Add(secondModel)
	require.NoError(t, err)
	count, err := treeentryinfodb.Count()
	require.NoError(t, err)
	condition = func() bool {
		return count == 2
	}
	require.Condition(t, condition, "Expected count to be 2", count)

	err = treeentryinfodb.Remove(secondModel)
	require.NoError(t, err)
	count, err = treeentryinfodb.Count()
	require.NoError(t, err)

	condition = func() bool {
		return count == 1
	}
	require.Condition(t, condition, "Expected count to be 1", count)

	model.Sequence = 100
	err = treeentryinfodb.Update(model)
	require.NoError(t, err)
	_, err = treeentryinfodb.Get(model)
	require.NoError(t, err)

	err = treeentryinfodb.Update(secondModel)
	require.NoError(t, err)

	condition = func() bool {
		return model.Sequence == 100
	}
	require.Condition(t, condition, "Inner exception")

	treeentryinfodb.Remove(model)
	count, err = treeentryinfodb.Count()
	require.NoError(t, err)

	condition = func() bool {
		return count == 0
	}
	require.Condition(t, condition, "Expected count to be 0", count)

	err = treeentryinfodb.DbConfig.CloseDb()
	isOpened = treeentryinfodb.DbConfig.DBIsOpened()
	require.NoError(t, err)

	condition = func() bool {
		return !isOpened
	}
	require.Condition(t, condition, "Expected db is not opened", isOpened)

	err = treeentryinfodb.DbConfig.DeleteDb()
	require.NoError(t, err)

	isExist := treeentryinfodb.DbConfig.DBIsExist()
	condition = func() bool {
		return !isExist
	}
	require.Condition(t, condition, "Expected db is not exist", isExist)
}
