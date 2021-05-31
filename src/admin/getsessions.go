package admin

import (
	"encoding/hex"
	"net"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type GetSessionsRequest struct{}

type GetSessionsResponse struct {
	Sessions map[string]SessionEntry `json:"sessions"`
}

type SessionEntry struct {
	PublicKey string `json:"key"`
}

func (a *AdminSocket) getSessionsHandler(req *GetSessionsRequest, res *GetSessionsResponse) error {
	res.Sessions = map[string]SessionEntry{}
	for _, s := range a.core.GetSessions() {
		addr := address.AddrForKey(s.Key)
		so := net.IP(addr[:]).String()
		res.Sessions[so] = SessionEntry{
			PublicKey: hex.EncodeToString(s.Key[:]),
		}
	}
	return nil
}
