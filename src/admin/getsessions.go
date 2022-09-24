package admin

import (
	"encoding/hex"
	"net"
	"sort"
	"strings"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type GetSessionsRequest struct{}

type GetSessionsResponse struct {
	Sessions []SessionEntry `json:"sessions"`
}

type SessionEntry struct {
	IPAddress string   `json:"address"`
	PublicKey string   `json:"key"`
	RXBytes   DataUnit `json:"bytes_recvd"`
	TXBytes   DataUnit `json:"bytes_sent"`
	Uptime    float64  `json:"uptime"`
}

func (a *AdminSocket) getSessionsHandler(req *GetSessionsRequest, res *GetSessionsResponse) error {
	sessions := a.core.GetSessions()
	res.Sessions = make([]SessionEntry, 0, len(sessions))
	for _, s := range sessions {
		addr := address.AddrForKey(s.Key)
		res.Sessions = append(res.Sessions, SessionEntry{
			IPAddress: net.IP(addr[:]).String(),
			PublicKey: hex.EncodeToString(s.Key[:]),
			RXBytes:   DataUnit(s.RXBytes),
			TXBytes:   DataUnit(s.TXBytes),
			Uptime:    s.Uptime.Seconds(),
		})
	}
	sort.SliceStable(res.Sessions, func(i, j int) bool {
		return strings.Compare(res.Sessions[i].PublicKey, res.Sessions[j].PublicKey) < 0
	})
	return nil
}
