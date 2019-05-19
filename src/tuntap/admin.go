package tuntap

import "github.com/yggdrasil-network/yggdrasil-go/src/admin"

func (t *TunAdapter) SetupAdminHandlers(a *admin.AdminSocket) {
	a.AddHandler("getTunTap", []string{}, func(in admin.Info) (r admin.Info, e error) {
		defer func() {
			if err := recover(); err != nil {
				r = admin.Info{"none": admin.Info{}}
				e = nil
			}
		}()

		return admin.Info{
			t.iface.Name(): admin.Info{
				"tap_mode": t.iface.IsTAP(),
				"mtu":      t.mtu,
			},
		}, nil
	})
	/*
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
		    a.AddHandler("getTunnelRouting", []string{}, func(in Info) (Info, error) {
		      enabled := false
		      a.core.router.doAdmin(func() {
		        enabled = a.core.router.cryptokey.isEnabled()
		      })
		      return Info{"enabled": enabled}, nil
		    })
		    a.AddHandler("setTunnelRouting", []string{"enabled"}, func(in Info) (Info, error) {
		      enabled := false
		      if e, ok := in["enabled"].(bool); ok {
		        enabled = e
		      }
		      a.core.router.doAdmin(func() {
		        a.core.router.cryptokey.setEnabled(enabled)
		      })
		      return Info{"enabled": enabled}, nil
		    })
		    a.AddHandler("addSourceSubnet", []string{"subnet"}, func(in Info) (Info, error) {
		      var err error
		      a.core.router.doAdmin(func() {
		        err = a.core.router.cryptokey.addSourceSubnet(in["subnet"].(string))
		      })
		      if err == nil {
		        return Info{"added": []string{in["subnet"].(string)}}, nil
		      } else {
		        return Info{"not_added": []string{in["subnet"].(string)}}, errors.New("Failed to add source subnet")
		      }
		    })
		    a.AddHandler("addRoute", []string{"subnet", "box_pub_key"}, func(in Info) (Info, error) {
		      var err error
		      a.core.router.doAdmin(func() {
		        err = a.core.router.cryptokey.addRoute(in["subnet"].(string), in["box_pub_key"].(string))
		      })
		      if err == nil {
		        return Info{"added": []string{fmt.Sprintf("%s via %s", in["subnet"].(string), in["box_pub_key"].(string))}}, nil
		      } else {
		        return Info{"not_added": []string{fmt.Sprintf("%s via %s", in["subnet"].(string), in["box_pub_key"].(string))}}, errors.New("Failed to add route")
		      }
		    })
		    a.AddHandler("getSourceSubnets", []string{}, func(in Info) (Info, error) {
		      var subnets []string
		      a.core.router.doAdmin(func() {
		        getSourceSubnets := func(snets []net.IPNet) {
		          for _, subnet := range snets {
		            subnets = append(subnets, subnet.String())
		          }
		        }
		        getSourceSubnets(a.core.router.cryptokey.ipv4sources)
		        getSourceSubnets(a.core.router.cryptokey.ipv6sources)
		      })
		      return Info{"source_subnets": subnets}, nil
		    })
		    a.AddHandler("getRoutes", []string{}, func(in Info) (Info, error) {
		      routes := make(Info)
		      a.core.router.doAdmin(func() {
		        getRoutes := func(ckrs []cryptokey_route) {
		          for _, ckr := range ckrs {
		            routes[ckr.subnet.String()] = hex.EncodeToString(ckr.destination[:])
		          }
		        }
		        getRoutes(a.core.router.cryptokey.ipv4routes)
		        getRoutes(a.core.router.cryptokey.ipv6routes)
		      })
		      return Info{"routes": routes}, nil
		    })
		    a.AddHandler("removeSourceSubnet", []string{"subnet"}, func(in Info) (Info, error) {
		      var err error
		      a.core.router.doAdmin(func() {
		        err = a.core.router.cryptokey.removeSourceSubnet(in["subnet"].(string))
		      })
		      if err == nil {
		        return Info{"removed": []string{in["subnet"].(string)}}, nil
		      } else {
		        return Info{"not_removed": []string{in["subnet"].(string)}}, errors.New("Failed to remove source subnet")
		      }
		    })
		    a.AddHandler("removeRoute", []string{"subnet", "box_pub_key"}, func(in Info) (Info, error) {
		      var err error
		      a.core.router.doAdmin(func() {
		        err = a.core.router.cryptokey.removeRoute(in["subnet"].(string), in["box_pub_key"].(string))
		      })
		      if err == nil {
		        return Info{"removed": []string{fmt.Sprintf("%s via %s", in["subnet"].(string), in["box_pub_key"].(string))}}, nil
		      } else {
		        return Info{"not_removed": []string{fmt.Sprintf("%s via %s", in["subnet"].(string), in["box_pub_key"].(string))}}, errors.New("Failed to remove route")
		      }
		    })
	*/
}
