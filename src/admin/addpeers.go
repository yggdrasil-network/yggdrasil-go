package admin

import (

	"fmt"
	
)

type AddPeersRequest struct {
	Uri  string   `json:"uri"`
	Intf string   `json:"intf"`
}

type AddPeersResponse struct {
	List []string `json:"list"`
}

func (a *AdminSocket) addPeersHandler(req *AddPeersRequest, res *AddPeersResponse) error {
	// Set sane defaults
	err:=a.core.AddPeer(req.Uri, req.Intf)
	if err != nil {
		fmt.Println("adding peer error %s", err)
		return err
	} else {
		fmt.Println("added peer %s", req.Uri)
		res.List = append(res.List, req.Uri)
	}

	return nil
}
