package admin

import (
	"encoding/hex"
	"net"
	"sort"
	"strings"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type GetPathsRequest struct {
}

type GetPathsResponse struct {
	Paths []PathEntry `json:"paths"`
}

type PathEntry struct {
	IPAddress string   `json:"address"`
	PublicKey string   `json:"key"`
	Path      []uint64 `json:"path"`
}

func (a *AdminSocket) getPathsHandler(req *GetPathsRequest, res *GetPathsResponse) error {
	paths := a.core.GetPaths()
	res.Paths = make([]PathEntry, 0, len(paths))
	for _, p := range paths {
		addr := address.AddrForKey(p.Key)
		res.Paths = append(res.Paths, PathEntry{
			IPAddress: net.IP(addr[:]).String(),
			PublicKey: hex.EncodeToString(p.Key),
			Path:      p.Path,
		})
	}
	sort.SliceStable(res.Paths, func(i, j int) bool {
		return strings.Compare(res.Paths[i].PublicKey, res.Paths[j].PublicKey) < 0
	})
	return nil
}
