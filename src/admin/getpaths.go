package admin

import (
	"encoding/hex"
	"net"
	"slices"
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
	Sequence  uint64   `json:"sequence"`
}

func (a *AdminSocket) getPathsHandler(_ *GetPathsRequest, res *GetPathsResponse) error {
	paths := a.core.GetPaths()
	res.Paths = make([]PathEntry, 0, len(paths))
	for _, p := range paths {
		addr := address.AddrForKey(p.Key)
		res.Paths = append(res.Paths, PathEntry{
			IPAddress: net.IP(addr[:]).String(),
			PublicKey: hex.EncodeToString(p.Key),
			Path:      p.Path,
			Sequence:  p.Sequence,
		})
	}
	slices.SortStableFunc(res.Paths, func(a, b PathEntry) int {
		return strings.Compare(a.PublicKey, b.PublicKey)
	})
	return nil
}
