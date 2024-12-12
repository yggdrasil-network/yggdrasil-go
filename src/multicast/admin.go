package multicast

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/Arceliar/phony"
	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
)

type GetMulticastInterfacesRequest struct{}
type GetMulticastInterfacesResponse struct {
	Interfaces []MulticastInterfaceState `json:"multicast_interfaces"`
}

type MulticastInterfaceState struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	Beacon   bool   `json:"beacon"`
	Listen   bool   `json:"listen"`
	Password bool   `json:"password"`
}

func (m *Multicast) getMulticastInterfacesHandler(_ *GetMulticastInterfacesRequest, res *GetMulticastInterfacesResponse) error {
	res.Interfaces = []MulticastInterfaceState{}
	phony.Block(m, func() {
		for name, intf := range m._interfaces {
			is := MulticastInterfaceState{
				Name:     intf.iface.Name,
				Beacon:   intf.beacon,
				Listen:   intf.listen,
				Password: len(intf.password) > 0,
			}
			if li := m._listeners[name]; li != nil && li.listener != nil {
				is.Address = li.listener.Addr().String()
			} else {
				is.Address = "-"
			}
			res.Interfaces = append(res.Interfaces, is)
		}
	})
	slices.SortStableFunc(res.Interfaces, func(a, b MulticastInterfaceState) int {
		return strings.Compare(a.Name, b.Name)
	})
	return nil
}

func (m *Multicast) SetupAdminHandlers(a *admin.AdminSocket) {
	_ = a.AddHandler(
		"getMulticastInterfaces", "Show which interfaces multicast is enabled on", []string{},
		func(in json.RawMessage) (interface{}, error) {
			req := &GetMulticastInterfacesRequest{}
			res := &GetMulticastInterfacesResponse{}
			if err := json.Unmarshal(in, &req); err != nil {
				return nil, err
			}
			if err := m.getMulticastInterfacesHandler(req, res); err != nil {
				return nil, err
			}
			return res, nil
		},
	)
}
