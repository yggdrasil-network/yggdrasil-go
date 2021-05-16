package multicast

import (
	"encoding/json"

	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
)

type GetMulticastInterfacesRequest struct{}
type GetMulticastInterfacesResponse struct {
	Interfaces []string `json:"multicast_interfaces"`
}

func (m *Multicast) getMulticastInterfacesHandler(req *GetMulticastInterfacesRequest, res *GetMulticastInterfacesResponse) error {
	res.Interfaces = []string{}
	for _, v := range m.Interfaces() {
		res.Interfaces = append(res.Interfaces, v.Name)
	}
	return nil
}

func (m *Multicast) SetupAdminHandlers(a *admin.AdminSocket) {
	_ = a.AddHandler("getMulticastInterfaces", []string{}, func(in json.RawMessage) (interface{}, error) {
		req := &GetMulticastInterfacesRequest{}
		res := &GetMulticastInterfacesResponse{}
		if err := json.Unmarshal(in, &req); err != nil {
			return nil, err
		}
		if err := m.getMulticastInterfacesHandler(req, res); err != nil {
			return nil, err
		}
		return res, nil
	})
}
