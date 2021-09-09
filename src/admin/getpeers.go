package admin

import (
	"encoding/hex"
	"net"

	"github.com/RiV-chain/RiV-mesh/src/address"
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
		}
	}
	return nil
}
