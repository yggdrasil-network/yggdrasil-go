package tun

import (
	"encoding/json"

	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
)

type GetTUNRequest struct{}
type GetTUNResponse struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name,omitempty"`
	MTU     uint64 `json:"mtu,omitempty"`
}

type TUNEntry struct {
	MTU uint64 `json:"mtu"`
}

func (t *TunAdapter) getTUNHandler(req *GetTUNRequest, res *GetTUNResponse) error {
	res.Enabled = t.isEnabled
	if !t.isEnabled {
		return nil
	}
	res.Name = t.Name()
	res.MTU = t.MTU()
	return nil
}

func (t *TunAdapter) SetupAdminHandlers(a *admin.AdminSocket) {
	_ = a.AddHandler(
		"getTun", "Show information about the node's TUN interface", []string{},
		func(in json.RawMessage) (interface{}, error) {
			req := &GetTUNRequest{}
			res := &GetTUNResponse{}
			if err := json.Unmarshal(in, &req); err != nil {
				return nil, err
			}
			if err := t.getTUNHandler(req, res); err != nil {
				return nil, err
			}
			return res, nil
		},
	)
}
