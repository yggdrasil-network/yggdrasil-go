package database

import (
	"context"
	"path/filepath"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	pathentryinfodb "github.com/yggdrasil-network/yggdrasil-go/src/db/PathEntryInfoDB"
	peerinfodb "github.com/yggdrasil-network/yggdrasil-go/src/db/PeerInfoDB"
	selfinfodb "github.com/yggdrasil-network/yggdrasil-go/src/db/SelfInfoDB"
	sessioninfodb "github.com/yggdrasil-network/yggdrasil-go/src/db/SessionInfoDB"
	treeentryinfodb "github.com/yggdrasil-network/yggdrasil-go/src/db/TreeEntryInfoDB"
	dbConfig "github.com/yggdrasil-network/yggdrasil-go/src/db/dbConfig"
)

type databaseConfig struct {
	treeentryinfodb       treeentryinfodb.TreeEntryInfoDBConfig
	sessioninfodb         sessioninfodb.SessionInfoDBConfig
	selfInfoDBConfig      selfinfodb.SelfInfoDBConfig
	pathEntryInfoDBConfig pathentryinfodb.PathEntryInfoDBConfig
	peerInfoDBConfig      peerinfodb.PeerInfoDBConfig
	ticker                *time.Ticker
	Api                   *core.Core
	Logger                core.Logger
}

var (
	path  = ""
	Timer = 5
)

func (db *databaseConfig) CreateTimer(ctx context.Context) {
	db.ticker = time.NewTicker(time.Duration(Timer) * time.Minute)
	go func() {
		defer db.ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				db.Logger.Infoln("Timer is stopped")
				_ = db.CloseDb()
			case <-db.ticker.C:
				db._backUpData()
			}
		}
	}()
}

func (db *databaseConfig) OnListen(ctx context.Context) {
	db.ticker = time.NewTicker(time.Duration(Timer) * time.Minute)
	go func() {
		defer db.ticker.Stop()
		sessions := make(map[string]core.SessionInfo)
		peers := make(map[string]core.PeerInfo)
		paths := make(map[string]core.PathEntryInfo)
		for {
			select {
			case <-ctx.Done():
				_ = db.CloseDb()
			case <-db.ticker.C:
				db._sessionBackUp(sessions)
				db._peerBackUp(peers)
				db._papthBackUp(paths)
			default:
				{
					APIsessions := db.Api.GetMappedSessions()
					APIpaths := db.Api.GetMappedPaths()
					for key, session := range APIsessions {
						if _, exist := sessions[key]; !exist {
							db._pathCallBack(APIpaths[key])
							db._sessionCallBack(session)
						}
						paths[key] = APIpaths[key]
						sessions[key] = session
					}
					for key := range sessions {
						if _, exist := APIsessions[key]; !exist {
							db._pathCallBack(APIpaths[key])
							db._sessionCallBack(sessions[key])
							delete(sessions, key)
							delete(paths, key)
						}
					}
				}
				{
					APIpeers := db.Api.GetMappedPeers()
					for key, peer := range APIpeers {
						if _, exist := peers[key]; !exist {
							db._peerCallBack(peer)
						}
						peers[key] = peer
					}
					for key := range peers {
						if _, exist := APIpeers[key]; !exist {
							db._peerCallBack(peers[key])
							delete(peers, key)
						}
					}
				}
			}
		}
	}()
}

func (db *databaseConfig) _sessionBackUp(sessions map[string]core.SessionInfo) {
	for _, session := range sessions {
		entity, err := dbConfig.NewSessionInfoDB(session)
		if err != nil {
			db.Logger.Errorf("Error creating SessionInfoDB: %v\n", err)
			return
		}
		_, err = db.sessioninfodb.Add(entity)
		if err != nil {
			db.Logger.Errorf("Error saving SessionInfoDB: %v\n", err)
			return
		}
	}
}

func (db *databaseConfig) _peerBackUp(peers map[string]core.PeerInfo) {
	for _, peer := range peers {
		entity, err := dbConfig.NewPeerInfoDB(peer)
		if err != nil {
			db.Logger.Errorf("Error creating PeerInfoDB: %v\n", err)
			return
		}
		_, err = db.peerInfoDBConfig.Add(entity)
		if err != nil {
			db.Logger.Errorf("Error saving PeerInfoDB: %v\n", err)
			return
		}
	}
}

func (db *databaseConfig) _papthBackUp(paths map[string]core.PathEntryInfo) {
	for _, path := range paths {
		entity, err := dbConfig.NewPathEntryInfoDB(path)
		if err != nil {
			db.Logger.Errorf("Error creating PathInfoDB: %v\n", err)
			return
		}
		_, err = db.pathEntryInfoDBConfig.Add(entity)
		if err != nil {
			db.Logger.Errorf("Error saving PathInfoDB: %v\n", err)
			return
		}
	}
}

func (db *databaseConfig) _sessionCallBack(session core.SessionInfo) {
	entity, err := dbConfig.NewSessionInfoDB(session)
	if err != nil {
		db.Logger.Errorf("Error creating SessionInfoDB: %v\n", err)
		return
	}
	_, err = db.sessioninfodb.Add(entity)
	if err != nil {
		db.Logger.Errorf("Error saving SessionInfoDB: %v\n", err)
		return
	}
}

func (db *databaseConfig) _peerCallBack(peer core.PeerInfo) {
	entity, err := dbConfig.NewPeerInfoDB(peer)
	if err != nil {
		db.Logger.Errorf("Error creating PeerInfoDB: %v\n", err)
		return
	}
	_, err = db.peerInfoDBConfig.Add(entity)
	if err != nil {
		db.Logger.Errorf("Error saving PeerInfoDB: %v\n", err)
		return
	}
}

func (db *databaseConfig) _pathCallBack(path core.PathEntryInfo) {
	entity, err := dbConfig.NewPathEntryInfoDB(path)
	if err != nil {
		db.Logger.Errorf("Error creating PathInfoDB: %v\n", err)
		return
	}
	_, err = db.pathEntryInfoDBConfig.Add(entity)
	if err != nil {
		db.Logger.Errorf("Error saving PathInfoDB: %v\n", err)
		return
	}
}

func (db *databaseConfig) _backUpData() {
	db.Logger.Infoln("Backup started")
	{
		selfinfo := db.Api.GetSelf()
		entity, _ := dbConfig.NewSelfInfoDB(selfinfo)
		db.selfInfoDBConfig.Add(entity)
	}
	{
		trees := db.Api.GetTree()
		id := uniqueId()
		for _, tree := range trees {
			entity, err := dbConfig.NewTreeEntryInfoDB(tree)
			entity.TreeId = id
			if err != nil {
				db.Logger.Errorln("Error creating TreeEntryInfoDB: %v\n", err)
				continue
			}
			db.treeentryinfodb.Add(entity)
		}
	}
	db.Logger.Infoln("Backup completed.")
}

func OpenExistDb(log core.Logger, core *core.Core) (_ *databaseConfig, err error) {
	if path != "" {
		path = addExtensionIfNotExist(path)
	}
	treeentryinfodb.Path = path
	sessioninfodb.Path = path
	pathentryinfodb.Path = path
	selfinfodb.Path = path
	peerinfodb.Path = path
	treeentrydb, err := treeentryinfodb.Open()
	if err != nil {
		return nil, err
	}
	sessiondb, err := sessioninfodb.Open()
	if err != nil {
		return nil, err
	}
	pathentry, err := pathentryinfodb.Open()
	if err != nil {
		return nil, err
	}
	selfinfodb, err := selfinfodb.Open()
	if err != nil {
		return nil, err
	}
	peerinfodb, err := peerinfodb.Open()
	if err != nil {
		return nil, err
	}
	db := &databaseConfig{
		treeentryinfodb:       *treeentrydb,
		sessioninfodb:         *sessiondb,
		selfInfoDBConfig:      *selfinfodb,
		pathEntryInfoDBConfig: *pathentry,
		peerInfoDBConfig:      *peerinfodb,
	}
	db.Logger = log
	db.Api = core
	return db, nil
}

func (db *databaseConfig) CloseDb() (errs []error) {
	err := db.treeentryinfodb.DbConfig.CloseDb()
	if err != nil {
		errs = append(errs, err)
	}
	err = db.sessioninfodb.DbConfig.CloseDb()
	if err != nil {
		errs = append(errs, err)
	}
	err = db.selfInfoDBConfig.DbConfig.CloseDb()
	if err != nil {
		errs = append(errs, err)
	}
	err = db.pathEntryInfoDBConfig.DbConfig.CloseDb()
	if err != nil {
		errs = append(errs, err)
	}
	err = db.peerInfoDBConfig.DbConfig.CloseDb()
	if err != nil {
		errs = append(errs, err)
	}
	return errs
}

func CreateDb(log core.Logger, core *core.Core) (_ *databaseConfig, err error) {
	if path != "" {
		path = addExtensionIfNotExist(path)
	}
	treeentryinfodb.Path = path
	sessioninfodb.Path = path
	pathentryinfodb.Path = path
	selfinfodb.Path = path
	peerinfodb.Path = path
	treeentrydb, err := treeentryinfodb.New()
	if err != nil {
		return nil, err
	}
	err = treeentrydb.DbConfig.OpenDb()
	if err != nil {
		return nil, err
	}
	sessiondb, err := sessioninfodb.New()
	if err != nil {
		return nil, err
	}
	err = sessiondb.DbConfig.OpenDb()
	if err != nil {
		return nil, err
	}
	pathentry, err := pathentryinfodb.New()
	if err != nil {
		return nil, err
	}
	err = pathentry.DbConfig.OpenDb()
	if err != nil {
		return nil, err
	}
	selfinfodb, err := selfinfodb.New()
	if err != nil {
		return nil, err
	}
	err = selfinfodb.DbConfig.OpenDb()
	if err != nil {
		return nil, err
	}
	peerinfodb, err := peerinfodb.New()
	if err != nil {
		return nil, err
	}
	err = peerinfodb.DbConfig.OpenDb()
	if err != nil {
		return nil, err
	}
	db := &databaseConfig{
		treeentryinfodb:       *treeentrydb,
		sessioninfodb:         *sessiondb,
		selfInfoDBConfig:      *selfinfodb,
		pathEntryInfoDBConfig: *pathentry,
		peerInfoDBConfig:      *peerinfodb,
	}
	db.Logger = log
	db.Api = core
	return db, nil
}

func addExtensionIfNotExist(fileName string) string {
	if !withExtension(fileName) {
		return addExtension(fileName)
	}
	return fileName
}

func withExtension(filePath string) bool {
	ext := filepath.Ext(filePath)
	return ext != ""
}
func addExtension(fileName string) string {
	return fileName + ".db"
}

func uniqueId() int {
	return int(time.Now().Unix())
}
