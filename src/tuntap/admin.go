package tuntap

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"

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
	a.AddHandler("getTunnelRouting", []string{}, func(in admin.Info) (admin.Info, error) {
		return admin.Info{"enabled": t.ckr.isEnabled()}, nil
	})
	a.AddHandler("setTunnelRouting", []string{"enabled"}, func(in admin.Info) (admin.Info, error) {
		enabled := false
		if e, ok := in["enabled"].(bool); ok {
			enabled = e
		}
		t.ckr.setEnabled(enabled)
		return admin.Info{"enabled": enabled}, nil
	})
	a.AddHandler("addLocalSubnet", []string{"subnet"}, func(in admin.Info) (admin.Info, error) {
		if err := t.ckr.addLocalSubnet(in["subnet"].(string)); err == nil {
			return admin.Info{"added": []string{in["subnet"].(string)}}, nil
		}
		return admin.Info{"not_added": []string{in["subnet"].(string)}}, errors.New("Failed to add source subnet")
	})
	a.AddHandler("addRemoteSubnet", []string{"subnet", "box_pub_key"}, func(in admin.Info) (admin.Info, error) {
		if err := t.ckr.addRemoteSubnet(in["subnet"].(string), in["box_pub_key"].(string)); err == nil {
			return admin.Info{"added": []string{fmt.Sprintf("%s via %s", in["subnet"].(string), in["box_pub_key"].(string))}}, nil
		}
		return admin.Info{"not_added": []string{fmt.Sprintf("%s via %s", in["subnet"].(string), in["box_pub_key"].(string))}}, errors.New("Failed to add route")
	})
	a.AddHandler("getSourceSubnets", []string{}, func(in admin.Info) (admin.Info, error) {
		var subnets []string
		getSourceSubnets := func(snets []net.IPNet) {
			for _, subnet := range snets {
				subnets = append(subnets, subnet.String())
			}
		}
		getSourceSubnets(t.ckr.ipv4locals)
		getSourceSubnets(t.ckr.ipv6locals)
		return admin.Info{"source_subnets": subnets}, nil
	})
	a.AddHandler("getRoutes", []string{}, func(in admin.Info) (admin.Info, error) {
		routes := make(admin.Info)
		getRoutes := func(ckrs []cryptokey_route) {
			for _, ckr := range ckrs {
				routes[ckr.subnet.String()] = hex.EncodeToString(ckr.destination[:])
			}
		}
		getRoutes(t.ckr.ipv4remotes)
		getRoutes(t.ckr.ipv6remotes)
		return admin.Info{"routes": routes}, nil
	})
	a.AddHandler("removeLocalSubnet", []string{"subnet"}, func(in admin.Info) (admin.Info, error) {
		if err := t.ckr.removeLocalSubnet(in["subnet"].(string)); err == nil {
			return admin.Info{"removed": []string{in["subnet"].(string)}}, nil
		}
		return admin.Info{"not_removed": []string{in["subnet"].(string)}}, errors.New("Failed to remove source subnet")
	})
	a.AddHandler("removeRemoteSubnet", []string{"subnet", "box_pub_key"}, func(in admin.Info) (admin.Info, error) {
		if err := t.ckr.removeRemoteSubnet(in["subnet"].(string), in["box_pub_key"].(string)); err == nil {
			return admin.Info{"removed": []string{fmt.Sprintf("%s via %s", in["subnet"].(string), in["box_pub_key"].(string))}}, nil
		}
		return admin.Info{"not_removed": []string{fmt.Sprintf("%s via %s", in["subnet"].(string), in["box_pub_key"].(string))}}, errors.New("Failed to remove route")
	})
}
