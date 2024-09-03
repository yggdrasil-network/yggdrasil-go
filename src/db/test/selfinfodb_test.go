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

	selfinfodb "github.com/yggdrasil-network/yggdrasil-go/src/db/SelfInfoDB"
)

func TestSelectSelfInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := selfinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	selfinfo := core.SelfInfo{
		Key: pubkey,
	}
	model, err := core.NewSelfInfoDB(selfinfo)
	require.NoError(t, err)

	rows := sqlmock.NewRows([]string{"RoutingEntries", "Key"}).
		AddRow(100, model.KeyBytes)

	mock.ExpectQuery("SELECT (.+) FROM self_info WHERE Id = \\?").
		WithArgs(model.Id).
		WillReturnRows(rows)

	_, err = cfg.Get(model)
	t.Log(err)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)

}

func TestInsertSelfInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := selfinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	selfinfo := core.SelfInfo{
		Key: pubkey,
	}
	model, err := core.NewSelfInfoDB(selfinfo)
	require.NoError(t, err)
	mock.ExpectExec("INSERT OR REPLACE INTO self_info").
		WithArgs(
			model.KeyBytes,
			model.RoutingEntries,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	_, err = cfg.Add(model)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestDeleteSelfInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := selfinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	selfinfo := core.SelfInfo{
		Key: pubkey,
	}
	model, err := core.NewSelfInfoDB(selfinfo)
	require.NoError(t, err)
	mock.ExpectExec("DELETE FROM self_info WHERE Id = \\?").
		WithArgs(
			model.Id,
		).WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.Remove(model)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestUpdateSelfInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := selfinfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	selfinfo := core.SelfInfo{
		Key:            pubkey,
		RoutingEntries: 100,
	}
	model, err := core.NewSelfInfoDB(selfinfo)
	require.NoError(t, err)
	mock.ExpectExec(`
		UPDATE self_info 
		SET 
			RoutingEntries = \?,
			Key = \?
		WHERE
			Id = \?`).
		WithArgs(
			model.RoutingEntries,
			model.KeyBytes,
			model.Id,
		).WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.Update(model)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestMainSelfInfo(t *testing.T) {
	selfinfodb.Name = fmt.Sprintf(
		"%s.%s",
		selfinfodb.Name,
		strconv.Itoa(int(time.Now().Unix())),
	)
	selfinfodb, err := selfinfodb.New()
	require.NoError(t, err)

	selfinfodb.DbConfig.OpenDb()
	isOpened := selfinfodb.DbConfig.DBIsOpened()
	condition := func() bool {
		return isOpened
	}
	require.Condition(t, condition, "Expected db is opened", isOpened)

	firstKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	secondKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	firstSelfinfo := core.SelfInfo{
		Key:            firstKey,
		RoutingEntries: 100,
	}
	firstModel, err := core.NewSelfInfoDB(firstSelfinfo)
	require.NoError(t, err)

	secondSelfinfo := core.SelfInfo{
		Key:            secondKey,
		RoutingEntries: 200,
	}
	secondModel, err := core.NewSelfInfoDB(secondSelfinfo)
	require.NoError(t, err)

	_, err = selfinfodb.Add(firstModel)
	require.NoError(t, err)
	_, err = selfinfodb.Add(secondModel)
	require.NoError(t, err)
	count, err := selfinfodb.Count()
	require.NoError(t, err)
	condition = func() bool {
		return count == 2
	}
	require.Condition(t, condition, "Expected count to be 2", count)

	err = selfinfodb.Remove(secondModel)
	require.NoError(t, err)
	count, err = selfinfodb.Count()
	require.NoError(t, err)

	condition = func() bool {
		return count == 1
	}
	require.Condition(t, condition, "Expected count to be 1", count)

	firstModel.RoutingEntries = 10
	err = selfinfodb.Update(firstModel)
	require.NoError(t, err)
	_, err = selfinfodb.Get(firstModel)
	require.NoError(t, err)

	err = selfinfodb.Update(secondModel)
	require.NoError(t, err)

	condition = func() bool {
		return firstModel.RoutingEntries == 10
	}
	require.Condition(t, condition, "Inner exception")

	selfinfodb.Remove(firstModel)
	count, err = selfinfodb.Count()
	require.NoError(t, err)

	condition = func() bool {
		return count == 0
	}
	require.Condition(t, condition, "Expected count to be 0", count)

	err = selfinfodb.DbConfig.CloseDb()
	isOpened = selfinfodb.DbConfig.DBIsOpened()
	require.NoError(t, err)

	condition = func() bool {
		return !isOpened
	}
	require.Condition(t, condition, "Expected db is not opened", isOpened)

	err = selfinfodb.DbConfig.DeleteDb()
	require.NoError(t, err)

	isExist := selfinfodb.DbConfig.DBIsExist()
	condition = func() bool {
		return !isExist
	}
	require.Condition(t, condition, "Expected db is not exist", isExist)
}
