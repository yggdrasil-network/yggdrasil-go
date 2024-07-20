package admin

import (
	"encoding/hex"
	"net"
	"sort"
	"strings"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type GetTreeRequest struct{}

type GetTreeResponse struct {
	Tree []TreeEntry `json:"tree"`
}

type TreeEntry struct {
	IPAddress string `json:"address"`
	PublicKey string `json:"key"`
	Parent    string `json:"parent"`
	Sequence  uint64 `json:"sequence"`
}

func (a *AdminSocket) getTreeHandler(_ *GetTreeRequest, res *GetTreeResponse) error {
	tree := a.core.GetTree()
	res.Tree = make([]TreeEntry, 0, len(tree))
	for _, d := range tree {
		addr := address.AddrForKey(d.Key)
		res.Tree = append(res.Tree, TreeEntry{
			IPAddress: net.IP(addr[:]).String(),
			PublicKey: hex.EncodeToString(d.Key[:]),
			Parent:    hex.EncodeToString(d.Parent[:]),
			Sequence:  d.Sequence,
		})
	}
	sort.SliceStable(res.Tree, func(i, j int) bool {
		return strings.Compare(res.Tree[i].PublicKey, res.Tree[j].PublicKey) < 0
	})
	return nil
}
