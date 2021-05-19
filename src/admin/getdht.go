package admin

import (
	"encoding/hex"
	"net"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type GetDHTRequest struct{}

type GetDHTResponse struct {
	DHT map[string]DHTEntry `json:"dht"`
}

type DHTEntry struct {
	PublicKey string `json:"key"`
	Port      uint64 `json:"port"`
	Rest      uint64 `json:"rest"`
}

func (a *AdminSocket) getDHTHandler(req *GetDHTRequest, res *GetDHTResponse) error {
	res.DHT = map[string]DHTEntry{}
	for _, d := range a.core.GetDHT() {
		addr := address.AddrForKey(d.Key)
		so := net.IP(addr[:]).String()
		res.DHT[so] = DHTEntry{
			PublicKey: hex.EncodeToString(d.Key[:]),
			Port:      d.Port,
			Rest:      d.Rest,
		}
	}
	return nil
}
