package admin

import (
	"encoding/hex"
	"net"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type GetPathsRequest struct {
}

type GetPathsResponse struct {
	Paths map[string]PathEntry `json:"paths"`
}

type PathEntry struct {
	PublicKey string   `json:"key"`
	Path      []uint64 `json:"path"`
}

func (a *AdminSocket) getPathsHandler(req *GetPathsRequest, res *GetPathsResponse) error {
	res.Paths = map[string]PathEntry{}
	for _, p := range a.core.GetPaths() {
		addr := address.AddrForKey(p.Key)
		so := net.IP(addr[:]).String()
		res.Paths[so] = PathEntry{
			PublicKey: hex.EncodeToString(p.Key),
			Path:      p.Path,
		}
	}
	return nil
}
