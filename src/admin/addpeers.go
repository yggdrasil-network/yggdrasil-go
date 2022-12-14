package admin

import (
	"fmt"
)

type AddPeersRequest struct {
	Uri  string `json:"uri"`
	Intf string `json:"intf"`
}

type AddPeersResponse struct {
	List []string `json:"list"`
}

func (a *AdminSocket) addPeersHandler(req *AddPeersRequest, res *AddPeersResponse) error {
	// Set sane defaults
	err := a.core.AddPeer(req.Uri, req.Intf)
	if err != nil {
		fmt.Printf("adding peer error %s\n", err)
		return err
	} else {
		fmt.Printf("added peer %s\n", req.Uri)
		res.List = append(res.List, req.Uri)
	}

	return nil
}
