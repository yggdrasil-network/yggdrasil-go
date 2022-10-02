package admin

type RemovePeerRequest struct {
	Uri   string `json:"uri"`
	Sintf string `json:"interface,omitempty"`
}

type RemovePeerResponse struct{}

func (a *AdminSocket) removePeerHandler(req *RemovePeerRequest, res *RemovePeerResponse) error {
	return a.core.RemovePeer(req.Uri, req.Sintf)
}
