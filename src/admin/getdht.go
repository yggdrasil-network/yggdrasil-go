package admin

import (
	"encoding/hex"
	"net"
	"sort"
	"strings"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type GetDHTRequest struct{}

type GetDHTResponse struct {
	DHT []DHTEntry `json:"dht"`
}

type DHTEntry struct {
	IPAddress string `json:"address"`
	PublicKey string `json:"key"`
	Port      uint64 `json:"port"`
	Rest      uint64 `json:"rest"`
}

func (a *AdminSocket) getDHTHandler(req *GetDHTRequest, res *GetDHTResponse) error {
	dht := a.core.GetDHT()
	res.DHT = make([]DHTEntry, 0, len(dht))
	for _, d := range dht {
		addr := address.AddrForKey(d.Key)
		res.DHT = append(res.DHT, DHTEntry{
			IPAddress: net.IP(addr[:]).String(),
			PublicKey: hex.EncodeToString(d.Key[:]),
			Port:      d.Port,
			Rest:      d.Rest,
		})
	}
	sort.SliceStable(res.DHT, func(i, j int) bool {
		return strings.Compare(res.DHT[i].PublicKey, res.DHT[j].PublicKey) < 0
	})
	return nil
}
