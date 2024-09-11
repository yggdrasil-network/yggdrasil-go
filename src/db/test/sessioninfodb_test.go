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

	sessioninfodb "github.com/yggdrasil-network/yggdrasil-go/src/db/SessionInfoDB"
)

func TestSelectSessionInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := sessioninfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.SessionInfo{
		Key:     pubkey,
		RXBytes: 10,
		TXBytes: 10,
		Uptime:  time.Hour,
	}
	model, err := core.NewSessionInfoDB(entry)
	require.NoError(t, err)

	rows := sqlmock.NewRows([]string{"RXBytes", "TXBytes", "Duration", "Key"}).
		AddRow(100, 200, 100, model.Key.GetPKIXPublicKeyBytes())

	mock.ExpectQuery("SELECT (.+) FROM session_info WHERE Id = \\?").
		WithArgs(model.Id).
		WillReturnRows(rows)

	_, err = cfg.Get(model)
	t.Log(err)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)

}

func TestInsertSessionInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := sessioninfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.SessionInfo{
		Key:     pubkey,
		RXBytes: 10,
		TXBytes: 10,
		Uptime:  time.Hour,
	}
	model, err := core.NewSessionInfoDB(entry)
	require.NoError(t, err)

	mock.ExpectExec("INSERT INTO session_info").
		WithArgs(
			model.Key.GetPKIXPublicKeyBytes(),
			model.RXBytes,
			model.TXBytes,
			model.Uptime,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	_, err = cfg.Add(model)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestDeleteSessionInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := sessioninfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.SessionInfo{
		Key:     pubkey,
		RXBytes: 10,
		TXBytes: 10,
		Uptime:  time.Hour,
	}
	model, err := core.NewSessionInfoDB(entry)
	require.NoError(t, err)
	mock.ExpectExec("DELETE FROM session_info WHERE Id = \\?").
		WithArgs(
			model.Id,
		).WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.Remove(model)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestUpdateSessionInfo(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	cfg, err := sessioninfodb.New()
	require.NoError(t, err)
	cfg.DbConfig.DB = mockDB

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.SessionInfo{
		Key:     pubkey,
		RXBytes: 10,
		TXBytes: 10,
		Uptime:  time.Hour,
	}
	model, err := core.NewSessionInfoDB(entry)
	require.NoError(t, err)
	mock.ExpectExec(`
		UPDATE session_info 
		SET 
			RXBytes = RXBytes \+ \?,
			TXBytes = TXBytes \+ \?,
			Duration = Duration \+ \?,
			Key = \?
		WHERE 
			Id = \?`).
		WithArgs(
			model.RXBytes,
			model.TXBytes,
			model.Uptime,
			model.Key.GetPKIXPublicKeyBytes(),
			model.Id,
		).WillReturnResult(sqlmock.NewResult(1, 1))

	err = cfg.Update(model)
	require.NoError(t, err)

	err = mock.ExpectationsWereMet()
	require.NoError(t, err)
}

func TestMainSessionInfo(t *testing.T) {
	sessioninfodb.Name = fmt.Sprintf(
		"%s.%s",
		sessioninfodb.Name,
		strconv.Itoa(int(time.Now().Unix())),
	)
	sessioninfodb, err := sessioninfodb.New()
	require.NoError(t, err)

	sessioninfodb.DbConfig.OpenDb()
	isOpened := sessioninfodb.DbConfig.DBIsOpened()
	condition := func() bool {
		return isOpened
	}
	require.Condition(t, condition, "Expected db is opened", isOpened)

	pubkey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	secondPubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	entry := core.SessionInfo{
		Key:     pubkey,
		RXBytes: 10,
		TXBytes: 10,
		Uptime:  time.Hour,
	}
	model, err := core.NewSessionInfoDB(entry)
	require.NoError(t, err)

	secondEntry := core.SessionInfo{
		Key:     secondPubKey,
		RXBytes: 10,
		TXBytes: 10,
		Uptime:  time.Hour,
	}
	secondModel, err := core.NewSessionInfoDB(secondEntry)
	require.NoError(t, err)

	_, err = sessioninfodb.Add(model)
	require.NoError(t, err)
	_, err = sessioninfodb.Add(secondModel)
	require.NoError(t, err)
	count, err := sessioninfodb.Count()
	require.NoError(t, err)
	condition = func() bool {
		return count == 2
	}
	require.Condition(t, condition, "Expected count to be 2", count)

	err = sessioninfodb.Remove(secondModel)
	require.NoError(t, err)
	count, err = sessioninfodb.Count()
	require.NoError(t, err)

	condition = func() bool {
		return count == 1
	}
	require.Condition(t, condition, "Expected count to be 1", count)

	model.TXBytes = 100
	model.RXBytes = 150
	model.Uptime = 100
	err = sessioninfodb.Update(model)
	require.NoError(t, err)
	_, err = sessioninfodb.Get(model)
	require.NoError(t, err)

	err = sessioninfodb.Update(secondModel)
	require.NoError(t, err)

	condition = func() bool {
		return model.RXBytes == 160 &&
			model.TXBytes == 110 && model.Uptime == 3600000000100
	}
	require.Condition(t, condition, "Inner exception")

	sessioninfodb.Remove(model)
	count, err = sessioninfodb.Count()
	require.NoError(t, err)

	condition = func() bool {
		return count == 0
	}
	require.Condition(t, condition, "Expected count to be 0", count)

	err = sessioninfodb.DbConfig.CloseDb()
	isOpened = sessioninfodb.DbConfig.DBIsOpened()
	require.NoError(t, err)

	condition = func() bool {
		return !isOpened
	}
	require.Condition(t, condition, "Expected db is not opened", isOpened)

	err = sessioninfodb.DbConfig.DeleteDb()
	require.NoError(t, err)

	isExist := sessioninfodb.DbConfig.DBIsExist()
	condition = func() bool {
		return !isExist
	}
	require.Condition(t, condition, "Expected db is not exist", isExist)
}
