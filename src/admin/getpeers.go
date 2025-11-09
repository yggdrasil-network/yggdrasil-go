package admin

import (
	"encoding/hex"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type GetPeersRequest struct {
	SortBy string `json:"sort"`
}

type GetPeersResponse struct {
	Peers []PeerEntry `json:"peers"`
}

type PeerEntry struct {
	URI           string        `json:"remote,omitempty"`
	Up            bool          `json:"up"`
	Inbound       bool          `json:"inbound"`
	IPAddress     string        `json:"address,omitempty"`
	PublicKey     string        `json:"key"`
	Port          uint64        `json:"port"`
	Priority      uint64        `json:"priority"`
	Cost          uint64        `json:"cost"`
	RXBytes       DataUnit      `json:"bytes_recvd,omitempty"`
	TXBytes       DataUnit      `json:"bytes_sent,omitempty"`
	RXRate        DataUnit      `json:"rate_recvd,omitempty"`
	TXRate        DataUnit      `json:"rate_sent,omitempty"`
	Uptime        float64       `json:"uptime,omitempty"`
	Latency       time.Duration `json:"latency,omitempty"`
	LastErrorTime time.Duration `json:"last_error_time,omitempty"`
	LastError     string        `json:"last_error,omitempty"`
}

func (a *AdminSocket) getPeersHandler(req *GetPeersRequest, res *GetPeersResponse) error {
	peers := a.core.GetPeers()
	res.Peers = make([]PeerEntry, 0, len(peers))
	for _, p := range peers {
		peer := PeerEntry{
			Port:     p.Port,
			Up:       p.Up,
			Inbound:  p.Inbound,
			Priority: uint64(p.Priority), // can't be uint8 thanks to gobind
			Cost:     p.Cost,
			URI:      p.URI,
			RXBytes:  DataUnit(p.RXBytes),
			TXBytes:  DataUnit(p.TXBytes),
			RXRate:   DataUnit(p.RXRate),
			TXRate:   DataUnit(p.TXRate),
			Uptime:   p.Uptime.Seconds(),
		}
		if p.Latency > 0 {
			peer.Latency = p.Latency
		}
		if addr := address.AddrForKey(p.Key); addr != nil {
			peer.PublicKey = hex.EncodeToString(p.Key)
			peer.IPAddress = net.IP(addr[:]).String()
		}
		if p.LastError != nil {
			peer.LastError = p.LastError.Error()
			peer.LastErrorTime = time.Since(p.LastErrorTime)
		}
		res.Peers = append(res.Peers, peer)
	}
	switch strings.ToLower(req.SortBy) {
	case "uptime":
		slices.SortStableFunc(res.Peers, sortByUptime)
	case "cost":
		slices.SortStableFunc(res.Peers, sortByCost)
	default:
		slices.SortStableFunc(res.Peers, sortByDefault)
	}
	return nil
}

func sortByDefault(a, b PeerEntry) int {
	if !a.Inbound && b.Inbound {
		return -1
	}
	if a.Inbound && !b.Inbound {
		return 1
	}
	if d := strings.Compare(a.PublicKey, b.PublicKey); d != 0 {
		return d
	}
	if d := a.Priority - b.Priority; d != 0 {
		return int(d)
	}
	if d := a.Cost - b.Cost; d != 0 {
		return int(d)
	}
	if d := a.Uptime - b.Uptime; d != 0 {
		return int(d)
	}
	return 0
}

func sortByCost(a, b PeerEntry) int {
	if d := a.Cost - b.Cost; d != 0 {
		return int(d)
	}
	if d := strings.Compare(a.PublicKey, b.PublicKey); d != 0 {
		return d
	}
	if d := a.Priority - b.Priority; d != 0 {
		return int(d)
	}
	if d := a.Uptime - b.Uptime; d != 0 {
		return int(d)
	}
	return 0
}

func sortByUptime(a, b PeerEntry) int {
	if d := a.Uptime - b.Uptime; d != 0 {
		return int(d)
	}
	if d := strings.Compare(a.PublicKey, b.PublicKey); d != 0 {
		return d
	}
	if d := a.Priority - b.Priority; d != 0 {
		return int(d)
	}
	if d := a.Cost - b.Cost; d != 0 {
		return int(d)
	}
	return 0
}
