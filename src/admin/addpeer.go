package admin

type AddPeerRequest struct {
	Uri   string `json:"uri"`
	Sintf string `json:"interface,omitempty"`
}

type AddPeerResponse struct{}

func (a *AdminSocket) addPeerHandler(req *AddPeerRequest, res *AddPeerResponse) error {
	return a.core.AddPeer(req.Uri, req.Sintf)
}
