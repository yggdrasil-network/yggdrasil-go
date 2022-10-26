package admin

import (
	"encoding/hex"
	"net"
	"sort"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type GetPeersRequest struct {
}

type GetPeersResponse struct {
	Peers []PeerEntry `json:"peers"`
}

type PeerEntry struct {
	IPAddress string   `json:"address"`
	PublicKey string   `json:"key"`
	Port      uint64   `json:"port"`
	Priority  uint8    `json:"priority"`
	Coords    []uint64 `json:"coords"`
	Remote    string   `json:"remote"`
	RXBytes   DataUnit `json:"bytes_recvd"`
	TXBytes   DataUnit `json:"bytes_sent"`
	Uptime    float64  `json:"uptime"`
}

func (a *AdminSocket) getPeersHandler(req *GetPeersRequest, res *GetPeersResponse) error {
	peers := a.core.GetPeers()
	res.Peers = make([]PeerEntry, 0, len(peers))
	for _, p := range peers {
		addr := address.AddrForKey(p.Key)
		res.Peers = append(res.Peers, PeerEntry{
			IPAddress: net.IP(addr[:]).String(),
			PublicKey: hex.EncodeToString(p.Key),
			Port:      p.Port,
			Priority:  p.Priority,
			Coords:    p.Coords,
			Remote:    p.Remote,
			RXBytes:   DataUnit(p.RXBytes),
			TXBytes:   DataUnit(p.TXBytes),
			Uptime:    p.Uptime.Seconds(),
		})
	}
	sort.Slice(res.Peers, func(i, j int) bool {
		if res.Peers[i].Port == res.Peers[j].Port {
			return res.Peers[i].Priority < res.Peers[j].Priority
		}
		return res.Peers[i].Port < res.Peers[j].Port
	})
	return nil
}
