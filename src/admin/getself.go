package admin

import (
	"crypto/ed25519"
	"encoding/hex"

	"github.com/yggdrasil-network/yggdrasil-go/src/version"
)

type GetSelfRequest struct{}

type GetSelfResponse struct {
	Self map[string]SelfEntry `json:"self"`
}

type SelfEntry struct {
	BuildName    string   `json:"build_name"`
	BuildVersion string   `json:"build_version"`
	PublicKey    string   `json:"key"`
	Coords       []uint64 `json:"coords"`
	Subnet       string   `json:"subnet"`
}

func (a *AdminSocket) getSelfHandler(req *GetSelfRequest, res *GetSelfResponse) error {
	res.Self = make(map[string]SelfEntry)
	public := a.core.PrivateKey().Public().(ed25519.PublicKey)
	addr := a.core.Address().String()
	snet := a.core.Subnet()
	res.Self[addr] = SelfEntry{
		BuildName:    version.BuildName(),
		BuildVersion: version.BuildVersion(),
		PublicKey:    hex.EncodeToString(public[:]),
		Subnet:       snet.String(),
		// TODO: coords
	}
	return nil
}
