package tuntap

import (
	//"encoding/hex"
	//"errors"
	//"fmt"
	//"net"

	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
)

func (t *TunAdapter) SetupAdminHandlers(a *admin.AdminSocket) {
	a.AddHandler("getTunTap", []string{}, func(in admin.Info) (r admin.Info, e error) {
		defer func() {
			if err := recover(); err != nil {
				r = admin.Info{"none": admin.Info{}}
				e = nil
			}
		}()

		return admin.Info{
			t.Name(): admin.Info{
				"mtu": t.mtu,
			},
		}, nil
	})
	/*
			// TODO: rewrite this as I'm fairly sure it doesn't work right on many
			// platforms anyway, but it may require changes to Water
		  a.AddHandler("setTunTap", []string{"name", "[tap_mode]", "[mtu]"}, func(in Info) (Info, error) {
		    // Set sane defaults
		    iftapmode := defaults.GetDefaults().DefaultIfTAPMode
		    ifmtu := defaults.GetDefaults().DefaultIfMTU
		    // Has TAP mode been specified?
		    if tap, ok := in["tap_mode"]; ok {
		      iftapmode = tap.(bool)
		    }
		    // Check we have enough params for MTU
		    if mtu, ok := in["mtu"]; ok {
		      if mtu.(float64) >= 1280 && ifmtu <= defaults.GetDefaults().MaximumIfMTU {
		        ifmtu = int(in["mtu"].(float64))
		      }
		    }
		    // Start the TUN adapter
		    if err := a.startTunWithMTU(in["name"].(string), iftapmode, ifmtu); err != nil {
		      return Info{}, errors.New("Failed to configure adapter")
		    } else {
		      return Info{
		        a.core.router.tun.iface.Name(): Info{
		          "tap_mode": a.core.router.tun.iface.IsTAP(),
		          "mtu":      ifmtu,
		        },
		      }, nil
		    }
		  })
	*/
}
