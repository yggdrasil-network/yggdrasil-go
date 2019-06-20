package multicast

import "github.com/yggdrasil-network/yggdrasil-go/src/admin"

func (m *Multicast) SetupAdminHandlers(a *admin.AdminSocket) {
	a.AddHandler("getMulticastInterfaces", []string{}, func(in admin.Info) (admin.Info, error) {
		var intfs []string
		for _, v := range m.GetInterfaces() {
			intfs = append(intfs, v.Name)
		}
		return admin.Info{"multicast_interfaces": intfs}, nil
	})
}
