package admin

import (
	"crypto/ed25519"
	"encoding/hex"

	"github.com/yggdrasil-network/yggdrasil-go/src/version"
)

type GetSelfRequest struct{}

type GetSelfResponse struct {
	BuildName    string   `json:"build_name"`
	BuildVersion string   `json:"build_version"`
	PublicKey    string   `json:"key"`
	Coords       []uint64 `json:"coords"`
	IPAddress    string   `json:"address"`
	Subnet       string   `json:"subnet"`
}

func (a *AdminSocket) getSelfHandler(req *GetSelfRequest, res *GetSelfResponse) error {
	res.BuildName = version.BuildName()
	res.BuildVersion = version.BuildVersion()
	public := a.core.PrivateKey().Public().(ed25519.PublicKey)
	res.PublicKey = hex.EncodeToString(public[:])
	res.IPAddress = a.core.Address().String()
	snet := a.core.Subnet()
	res.Subnet = snet.String()
	// TODO: res.coords
	return nil
}
