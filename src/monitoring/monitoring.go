package monitoring

import (
	"net"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

type Monitoring struct {
	core *core.Core
	done chan struct{}
	log  core.Logger
	once sync.Once
}

func New(c *core.Core, log core.Logger) (*Monitoring, error) {
	m := &Monitoring{
		core: c,
		log:  log,
	}
	m.done = make(chan struct{})
	go m.Monitoring(func(peer core.PeerInfo) {
		log.Printf("Peers: %s", peer.URI)
	}, func(session core.SessionInfo) {
		addr := address.AddrForKey(session.Key)
		addrStr := net.IP(addr[:]).String()
		log.Printf("Session: %s", addrStr)
	})
	return m, nil
}

func (m *Monitoring) Stop() error {
	if m == nil {
		return nil
	}
	m.once.Do(func() {
		close(m.done)
	})
	return nil
}

type PeerMonitoring func(peer core.PeerInfo)
type SessionMonitoring func(peer core.SessionInfo)

func (m *Monitoring) Monitoring(PeerMonitoringCallBack PeerMonitoring, SessionMonitoringCallBack SessionMonitoring) {
	peers := make(map[string]struct{})
	sessions := make(map[string]struct{})
	for {
		APIpeers := m.core.GetPeers()
		for _, peer := range APIpeers {
			if _, exist := peers[peer.URI]; !exist {
				PeerMonitoringCallBack(peer)
				peers[peer.URI] = struct{}{}
			}
		}
		APIsessions := m.core.GetSessions()
		for _, session := range APIsessions {
			addr := address.AddrForKey(session.Key)
			addrStr := net.IP(addr[:]).String()
			if _, exist := sessions[addrStr]; !exist {
				SessionMonitoringCallBack(session)
				sessions[addrStr] = struct{}{}
			}
		}
		select {
		case <-m.done:
			return
		default:
		}
	}
}
