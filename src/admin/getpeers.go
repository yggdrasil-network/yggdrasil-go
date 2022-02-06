package admin

import (
	"encoding/hex"
	"net"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type GetPeersRequest struct {
}

type GetPeersResponse struct {
	Peers map[string]PeerEntry `json:"peers"`
}

type PeerEntry struct {
	PublicKey string   `json:"key"`
	Port      uint64   `json:"port"`
	Coords    []uint64 `json:"coords"`
	Remote    string   `json:"remote"`
	RXBytes   uint64   `json:"bytes_recvd"`
	TXBytes   uint64   `json:"bytes_sent"`
	Uptime    float64  `json:"uptime"`
}

func (a *AdminSocket) getPeersHandler(req *GetPeersRequest, res *GetPeersResponse) error {
	res.Peers = map[string]PeerEntry{}
	for _, p := range a.core.GetPeers() {
		addr := address.AddrForKey(p.Key)
		so := net.IP(addr[:]).String()
		res.Peers[so] = PeerEntry{
			PublicKey: hex.EncodeToString(p.Key),
			Port:      p.Port,
			Coords:    p.Coords,
			Remote:    p.Remote,
			RXBytes:   p.RXBytes,
			TXBytes:   p.TXBytes,
			Uptime:    p.Uptime.Seconds(),
		}
	}
	return nil
}
