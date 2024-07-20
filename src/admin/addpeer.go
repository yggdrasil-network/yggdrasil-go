package admin

import (
	"fmt"
	"net/url"
)

type AddPeerRequest struct {
	Uri   string `json:"uri"`
	Sintf string `json:"interface,omitempty"`
}

type AddPeerResponse struct{}

func (a *AdminSocket) addPeerHandler(req *AddPeerRequest, _ *AddPeerResponse) error {
	u, err := url.Parse(req.Uri)
	if err != nil {
		return fmt.Errorf("unable to parse peering URI: %w", err)
	}
	return a.core.AddPeer(u, req.Sintf)
}
