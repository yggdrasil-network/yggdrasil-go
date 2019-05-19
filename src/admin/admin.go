package admin

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

// TODO: Add authentication

type AdminSocket struct {
	core        *yggdrasil.Core
	log         *log.Logger
	reconfigure chan chan error
	listenaddr  string
	listener    net.Listener
	handlers    []admin_handlerInfo
}

type admin_info map[string]interface{}

type admin_handlerInfo struct {
	name    string                               // Checked against the first word of the api call
	args    []string                             // List of human-readable argument names
	handler func(admin_info) (admin_info, error) // First is input map, second is output
}

// admin_pair maps things like "IP", "port", "bucket", or "coords" onto values.
type admin_pair struct {
	key string
	val interface{}
}

// admin_nodeInfo represents the information we know about a node for an admin response.
type admin_nodeInfo []admin_pair

// addHandler is called for each admin function to add the handler and help documentation to the API.
func (a *AdminSocket) addHandler(name string, args []string, handler func(admin_info) (admin_info, error)) {
	a.handlers = append(a.handlers, admin_handlerInfo{name, args, handler})
}

// init runs the initial admin setup.
func (a *AdminSocket) Init(c *yggdrasil.Core, state *config.NodeState, log *log.Logger, options interface{}) {
	a.core = c
	a.log = log
	a.reconfigure = make(chan chan error, 1)
	go func() {
		for {
			e := <-a.reconfigure
			current, previous := state.Get()
			if current.AdminListen != previous.AdminListen {
				a.listenaddr = current.AdminListen
				a.Stop()
				a.Start()
			}
			e <- nil
		}
	}()
	current, _ := state.Get()
	a.listenaddr = current.AdminListen
	a.addHandler("list", []string{}, func(in admin_info) (admin_info, error) {
		handlers := make(map[string]interface{})
		for _, handler := range a.handlers {
			handlers[handler.name] = admin_info{"fields": handler.args}
		}
		return admin_info{"list": handlers}, nil
	})
	/*a.addHandler("dot", []string{}, func(in admin_info) (admin_info, error) {
		return admin_info{"dot": string(a.getResponse_dot())}, nil
	})*/
	a.addHandler("getSelf", []string{}, func(in admin_info) (admin_info, error) {
		ip := c.Address().String()
		return admin_info{
			"self": admin_info{
				ip: admin_info{
					"box_pub_key":   c.BoxPubKey(),
					"build_name":    yggdrasil.BuildName(),
					"build_version": yggdrasil.BuildVersion(),
					"coords":        fmt.Sprintf("%v", c.Coords()),
					"subnet":        c.Subnet().String(),
				},
			},
		}, nil
	})
	a.addHandler("getPeers", []string{}, func(in admin_info) (admin_info, error) {
		peers := make(admin_info)
		for _, p := range a.core.GetPeers() {
			addr := *address.AddrForNodeID(crypto.GetNodeID(&p.PublicKey))
			so := net.IP(addr[:]).String()
			peers[so] = admin_info{
				"ip":          so,
				"port":        p.Port,
				"uptime":      p.Uptime.Seconds(),
				"bytes_sent":  p.BytesSent,
				"bytes_recvd": p.BytesRecvd,
				"proto":       p.Protocol,
				"endpoint":    p.Endpoint,
				"box_pub_key": p.PublicKey,
			}
		}
		return admin_info{"peers": peers}, nil
	})
	a.addHandler("getSwitchPeers", []string{}, func(in admin_info) (admin_info, error) {
		switchpeers := make(admin_info)
		for _, s := range a.core.GetSwitchPeers() {
			addr := *address.AddrForNodeID(crypto.GetNodeID(&s.PublicKey))
			so := fmt.Sprint(s.Port)
			switchpeers[so] = admin_info{
				"ip":          net.IP(addr[:]).String(),
				"coords":      fmt.Sprintf("%v", s.Coords),
				"port":        s.Port,
				"bytes_sent":  s.BytesSent,
				"bytes_recvd": s.BytesRecvd,
				"proto":       s.Protocol,
				"endpoint":    s.Endpoint,
				"box_pub_key": s.PublicKey,
			}
		}
		return admin_info{"switchpeers": switchpeers}, nil
	})
	/*a.addHandler("getSwitchQueues", []string{}, func(in admin_info) (admin_info, error) {
		queues := a.core.GetSwitchQueues()
		return admin_info{"switchqueues": queues.asMap()}, nil
	})*/
	a.addHandler("getDHT", []string{}, func(in admin_info) (admin_info, error) {
		dht := make(admin_info)
		for _, d := range a.core.GetDHT() {
			addr := *address.AddrForNodeID(crypto.GetNodeID(&d.PublicKey))
			so := net.IP(addr[:]).String()
			dht[so] = admin_info{
				"coords":      fmt.Sprintf("%v", d.Coords),
				"last_seen":   d.LastSeen.Seconds(),
				"box_pub_key": d.PublicKey,
			}
		}
		return admin_info{"dht": dht}, nil
	})
	a.addHandler("getSessions", []string{}, func(in admin_info) (admin_info, error) {
		sessions := make(admin_info)
		for _, s := range a.core.GetSessions() {
			addr := *address.AddrForNodeID(crypto.GetNodeID(&s.PublicKey))
			so := net.IP(addr[:]).String()
			sessions[so] = admin_info{
				"coords":        fmt.Sprintf("%v", s.Coords),
				"bytes_sent":    s.BytesSent,
				"bytes_recvd":   s.BytesRecvd,
				"mtu":           s.MTU,
				"was_mtu_fixed": s.WasMTUFixed,
				"box_pub_key":   s.PublicKey,
			}
		}
		return admin_info{"sessions": sessions}, nil
	})
	/*a.addHandler("addPeer", []string{"uri", "[interface]"}, func(in admin_info) (admin_info, error) {
		// Set sane defaults
		intf := ""
		// Has interface been specified?
		if itf, ok := in["interface"]; ok {
			intf = itf.(string)
		}
		if a.addPeer(in["uri"].(string), intf) == nil {
			return admin_info{
				"added": []string{
					in["uri"].(string),
				},
			}, nil
		} else {
			return admin_info{
				"not_added": []string{
					in["uri"].(string),
				},
			}, errors.New("Failed to add peer")
		}
	})
	a.addHandler("removePeer", []string{"port"}, func(in admin_info) (admin_info, error) {
		if a.removePeer(fmt.Sprint(in["port"])) == nil {
			return admin_info{
				"removed": []string{
					fmt.Sprint(in["port"]),
				},
			}, nil
		} else {
			return admin_info{
				"not_removed": []string{
					fmt.Sprint(in["port"]),
				},
			}, errors.New("Failed to remove peer")
		}
	})
		a.addHandler("getTunTap", []string{}, func(in admin_info) (r admin_info, e error) {
			defer func() {
				if err := recover(); err != nil {
					r = admin_info{"none": admin_info{}}
					e = nil
				}
			}()

			return admin_info{
				a.core.router.tun.iface.Name(): admin_info{
					"tap_mode": a.core.router.tun.iface.IsTAP(),
					"mtu":      a.core.router.tun.mtu,
				},
			}, nil
		})
		a.addHandler("setTunTap", []string{"name", "[tap_mode]", "[mtu]"}, func(in admin_info) (admin_info, error) {
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
				return admin_info{}, errors.New("Failed to configure adapter")
			} else {
				return admin_info{
					a.core.router.tun.iface.Name(): admin_info{
						"tap_mode": a.core.router.tun.iface.IsTAP(),
						"mtu":      ifmtu,
					},
				}, nil
			}
		})*/
	/*a.addHandler("getMulticastInterfaces", []string{}, func(in admin_info) (admin_info, error) {
		var intfs []string
		for _, v := range a.core.multicast.interfaces() {
			intfs = append(intfs, v.Name)
		}
		return admin_info{"multicast_interfaces": intfs}, nil
	})
	a.addHandler("getAllowedEncryptionPublicKeys", []string{}, func(in admin_info) (admin_info, error) {
		return admin_info{"allowed_box_pubs": a.getAllowedEncryptionPublicKeys()}, nil
	})
	a.addHandler("addAllowedEncryptionPublicKey", []string{"box_pub_key"}, func(in admin_info) (admin_info, error) {
		if a.addAllowedEncryptionPublicKey(in["box_pub_key"].(string)) == nil {
			return admin_info{
				"added": []string{
					in["box_pub_key"].(string),
				},
			}, nil
		} else {
			return admin_info{
				"not_added": []string{
					in["box_pub_key"].(string),
				},
			}, errors.New("Failed to add allowed key")
		}
	})
	a.addHandler("removeAllowedEncryptionPublicKey", []string{"box_pub_key"}, func(in admin_info) (admin_info, error) {
		if a.removeAllowedEncryptionPublicKey(in["box_pub_key"].(string)) == nil {
			return admin_info{
				"removed": []string{
					in["box_pub_key"].(string),
				},
			}, nil
		} else {
			return admin_info{
				"not_removed": []string{
					in["box_pub_key"].(string),
				},
			}, errors.New("Failed to remove allowed key")
		}
	})
	a.addHandler("getTunnelRouting", []string{}, func(in admin_info) (admin_info, error) {
		enabled := false
		a.core.router.doAdmin(func() {
			enabled = a.core.router.cryptokey.isEnabled()
		})
		return admin_info{"enabled": enabled}, nil
	})
	a.addHandler("setTunnelRouting", []string{"enabled"}, func(in admin_info) (admin_info, error) {
		enabled := false
		if e, ok := in["enabled"].(bool); ok {
			enabled = e
		}
		a.core.router.doAdmin(func() {
			a.core.router.cryptokey.setEnabled(enabled)
		})
		return admin_info{"enabled": enabled}, nil
	})
	a.addHandler("addSourceSubnet", []string{"subnet"}, func(in admin_info) (admin_info, error) {
		var err error
		a.core.router.doAdmin(func() {
			err = a.core.router.cryptokey.addSourceSubnet(in["subnet"].(string))
		})
		if err == nil {
			return admin_info{"added": []string{in["subnet"].(string)}}, nil
		} else {
			return admin_info{"not_added": []string{in["subnet"].(string)}}, errors.New("Failed to add source subnet")
		}
	})
	a.addHandler("addRoute", []string{"subnet", "box_pub_key"}, func(in admin_info) (admin_info, error) {
		var err error
		a.core.router.doAdmin(func() {
			err = a.core.router.cryptokey.addRoute(in["subnet"].(string), in["box_pub_key"].(string))
		})
		if err == nil {
			return admin_info{"added": []string{fmt.Sprintf("%s via %s", in["subnet"].(string), in["box_pub_key"].(string))}}, nil
		} else {
			return admin_info{"not_added": []string{fmt.Sprintf("%s via %s", in["subnet"].(string), in["box_pub_key"].(string))}}, errors.New("Failed to add route")
		}
	})
	a.addHandler("getSourceSubnets", []string{}, func(in admin_info) (admin_info, error) {
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
		return admin_info{"source_subnets": subnets}, nil
	})
	a.addHandler("getRoutes", []string{}, func(in admin_info) (admin_info, error) {
		routes := make(admin_info)
		a.core.router.doAdmin(func() {
			getRoutes := func(ckrs []cryptokey_route) {
				for _, ckr := range ckrs {
					routes[ckr.subnet.String()] = hex.EncodeToString(ckr.destination[:])
				}
			}
			getRoutes(a.core.router.cryptokey.ipv4routes)
			getRoutes(a.core.router.cryptokey.ipv6routes)
		})
		return admin_info{"routes": routes}, nil
	})
	a.addHandler("removeSourceSubnet", []string{"subnet"}, func(in admin_info) (admin_info, error) {
		var err error
		a.core.router.doAdmin(func() {
			err = a.core.router.cryptokey.removeSourceSubnet(in["subnet"].(string))
		})
		if err == nil {
			return admin_info{"removed": []string{in["subnet"].(string)}}, nil
		} else {
			return admin_info{"not_removed": []string{in["subnet"].(string)}}, errors.New("Failed to remove source subnet")
		}
	})
	a.addHandler("removeRoute", []string{"subnet", "box_pub_key"}, func(in admin_info) (admin_info, error) {
		var err error
		a.core.router.doAdmin(func() {
			err = a.core.router.cryptokey.removeRoute(in["subnet"].(string), in["box_pub_key"].(string))
		})
		if err == nil {
			return admin_info{"removed": []string{fmt.Sprintf("%s via %s", in["subnet"].(string), in["box_pub_key"].(string))}}, nil
		} else {
			return admin_info{"not_removed": []string{fmt.Sprintf("%s via %s", in["subnet"].(string), in["box_pub_key"].(string))}}, errors.New("Failed to remove route")
		}
	})
	a.addHandler("dhtPing", []string{"box_pub_key", "coords", "[target]"}, func(in admin_info) (admin_info, error) {
		if in["target"] == nil {
			in["target"] = "none"
		}
		result, err := a.admin_dhtPing(in["box_pub_key"].(string), in["coords"].(string), in["target"].(string))
		if err == nil {
			infos := make(map[string]map[string]string, len(result.Infos))
			for _, dinfo := range result.Infos {
				info := map[string]string{
					"box_pub_key": hex.EncodeToString(dinfo.key[:]),
					"coords":      fmt.Sprintf("%v", dinfo.coords),
				}
				addr := net.IP(address.AddrForNodeID(crypto.GetNodeID(&dinfo.key))[:]).String()
				infos[addr] = info
			}
			return admin_info{"nodes": infos}, nil
		} else {
			return admin_info{}, err
		}
	})
	a.addHandler("getNodeInfo", []string{"[box_pub_key]", "[coords]", "[nocache]"}, func(in admin_info) (admin_info, error) {
		var nocache bool
		if in["nocache"] != nil {
			nocache = in["nocache"].(string) == "true"
		}
		var box_pub_key, coords string
		if in["box_pub_key"] == nil && in["coords"] == nil {
			var nodeinfo []byte
			a.core.router.doAdmin(func() {
				nodeinfo = []byte(a.core.router.nodeinfo.getNodeInfo())
			})
			var jsoninfo interface{}
			if err := json.Unmarshal(nodeinfo, &jsoninfo); err != nil {
				return admin_info{}, err
			} else {
				return admin_info{"nodeinfo": jsoninfo}, nil
			}
		} else if in["box_pub_key"] == nil || in["coords"] == nil {
			return admin_info{}, errors.New("Expecting both box_pub_key and coords")
		} else {
			box_pub_key = in["box_pub_key"].(string)
			coords = in["coords"].(string)
		}
		result, err := a.admin_getNodeInfo(box_pub_key, coords, nocache)
		if err == nil {
			var m map[string]interface{}
			if err = json.Unmarshal(result, &m); err == nil {
				return admin_info{"nodeinfo": m}, nil
			} else {
				return admin_info{}, err
			}
		} else {
			return admin_info{}, err
		}
	})*/
}

// start runs the admin API socket to listen for / respond to admin API calls.
func (a *AdminSocket) Start() error {
	if a.listenaddr != "none" && a.listenaddr != "" {
		go a.listen()
	}
	return nil
}

// cleans up when stopping
func (a *AdminSocket) Stop() error {
	if a.listener != nil {
		return a.listener.Close()
	} else {
		return nil
	}
}

// listen is run by start and manages API connections.
func (a *AdminSocket) listen() {
	u, err := url.Parse(a.listenaddr)
	if err == nil {
		switch strings.ToLower(u.Scheme) {
		case "unix":
			if _, err := os.Stat(a.listenaddr[7:]); err == nil {
				a.log.Debugln("Admin socket", a.listenaddr[7:], "already exists, trying to clean up")
				if _, err := net.DialTimeout("unix", a.listenaddr[7:], time.Second*2); err == nil || err.(net.Error).Timeout() {
					a.log.Errorln("Admin socket", a.listenaddr[7:], "already exists and is in use by another process")
					os.Exit(1)
				} else {
					if err := os.Remove(a.listenaddr[7:]); err == nil {
						a.log.Debugln(a.listenaddr[7:], "was cleaned up")
					} else {
						a.log.Errorln(a.listenaddr[7:], "already exists and was not cleaned up:", err)
						os.Exit(1)
					}
				}
			}
			a.listener, err = net.Listen("unix", a.listenaddr[7:])
			if err == nil {
				switch a.listenaddr[7:8] {
				case "@": // maybe abstract namespace
				default:
					if err := os.Chmod(a.listenaddr[7:], 0660); err != nil {
						a.log.Warnln("WARNING:", a.listenaddr[:7], "may have unsafe permissions!")
					}
				}
			}
		case "tcp":
			a.listener, err = net.Listen("tcp", u.Host)
		default:
			// err = errors.New(fmt.Sprint("protocol not supported: ", u.Scheme))
			a.listener, err = net.Listen("tcp", a.listenaddr)
		}
	} else {
		a.listener, err = net.Listen("tcp", a.listenaddr)
	}
	if err != nil {
		a.log.Errorf("Admin socket failed to listen: %v", err)
		os.Exit(1)
	}
	a.log.Infof("%s admin socket listening on %s",
		strings.ToUpper(a.listener.Addr().Network()),
		a.listener.Addr().String())
	defer a.listener.Close()
	for {
		conn, err := a.listener.Accept()
		if err == nil {
			go a.handleRequest(conn)
		}
	}
}

// handleRequest calls the request handler for each request sent to the admin API.
func (a *AdminSocket) handleRequest(conn net.Conn) {
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	encoder.SetIndent("", "  ")
	recv := make(admin_info)
	send := make(admin_info)

	defer func() {
		r := recover()
		if r != nil {
			send = admin_info{
				"status": "error",
				"error":  "Unrecoverable error, possibly as a result of invalid input types or malformed syntax",
			}
			a.log.Errorln("Admin socket error:", r)
			if err := encoder.Encode(&send); err != nil {
				a.log.Errorln("Admin socket JSON encode error:", err)
			}
			conn.Close()
		}
	}()

	for {
		// Start with a clean slate on each request
		recv = admin_info{}
		send = admin_info{}

		// Decode the input
		if err := decoder.Decode(&recv); err != nil {
			//	fmt.Println("Admin socket JSON decode error:", err)
			return
		}

		// Send the request back with the response, and default to "error"
		// unless the status is changed below by one of the handlers
		send["request"] = recv
		send["status"] = "error"

	handlers:
		for _, handler := range a.handlers {
			// We've found the handler that matches the request
			if strings.ToLower(recv["request"].(string)) == strings.ToLower(handler.name) {
				// Check that we have all the required arguments
				for _, arg := range handler.args {
					// An argument in [square brackets] is optional and not required,
					// so we can safely ignore those
					if strings.HasPrefix(arg, "[") && strings.HasSuffix(arg, "]") {
						continue
					}
					// Check if the field is missing
					if _, ok := recv[arg]; !ok {
						send = admin_info{
							"status":    "error",
							"error":     "Expected field missing: " + arg,
							"expecting": arg,
						}
						break handlers
					}
				}

				// By this point we should have all the fields we need, so call
				// the handler
				response, err := handler.handler(recv)
				if err != nil {
					send["error"] = err.Error()
					if response != nil {
						send["response"] = response
					}
				} else {
					send["status"] = "success"
					if response != nil {
						send["response"] = response
					}
				}

				break
			}
		}

		// Send the response back
		if err := encoder.Encode(&send); err != nil {
			return
		}

		// If "keepalive" isn't true then close the connection
		if keepalive, ok := recv["keepalive"]; !ok || !keepalive.(bool) {
			conn.Close()
		}
	}
}

// asMap converts an admin_nodeInfo into a map of key/value pairs.
func (n *admin_nodeInfo) asMap() map[string]interface{} {
	m := make(map[string]interface{}, len(*n))
	for _, p := range *n {
		m[p.key] = p.val
	}
	return m
}

// toString creates a printable string representation of an admin_nodeInfo.
func (n *admin_nodeInfo) toString() string {
	// TODO return something nicer looking than this
	var out []string
	for _, p := range *n {
		out = append(out, fmt.Sprintf("%v: %v", p.key, p.val))
	}
	return strings.Join(out, ", ")
}

// printInfos returns a newline separated list of strings from admin_nodeInfos, e.g. a printable string of info about all peers.
func (a *AdminSocket) printInfos(infos []admin_nodeInfo) string {
	var out []string
	for _, info := range infos {
		out = append(out, info.toString())
	}
	out = append(out, "") // To add a trailing "\n" in the join
	return strings.Join(out, "\n")
}

/*
// addPeer triggers a connection attempt to a node.
func (a *AdminSocket) addPeer(addr string, sintf string) error {
	err := a.core.link.call(addr, sintf)
	if err != nil {
		return err
	}
	return nil
}

// removePeer disconnects an existing node (given by the node's port number).
func (a *AdminSocket) removePeer(p string) error {
	iport, err := strconv.Atoi(p)
	if err != nil {
		return err
	}
	a.core.RemovePeer(iport)
	return nil
}
*/
/*
// Send a DHT ping to the node with the provided key and coords, optionally looking up the specified target NodeID.
func (a *AdminSocket) admin_dhtPing(keyString, coordString, targetString string) (dhtRes, error) {
	var key crypto.BoxPubKey
	if keyBytes, err := hex.DecodeString(keyString); err != nil {
		return dhtRes{}, err
	} else {
		copy(key[:], keyBytes)
	}
	var coords []byte
	for _, cstr := range strings.Split(strings.Trim(coordString, "[]"), " ") {
		if cstr == "" {
			// Special case, happens if trimmed is the empty string, e.g. this is the root
			continue
		}
		if u64, err := strconv.ParseUint(cstr, 10, 8); err != nil {
			return dhtRes{}, err
		} else {
			coords = append(coords, uint8(u64))
		}
	}
	resCh := make(chan *dhtRes, 1)
	info := dhtInfo{
		key:    key,
		coords: coords,
	}
	target := *info.getNodeID()
	if targetString == "none" {
		// Leave the default target in place
	} else if targetBytes, err := hex.DecodeString(targetString); err != nil {
		return dhtRes{}, err
	} else if len(targetBytes) != len(target) {
		return dhtRes{}, errors.New("Incorrect target NodeID length")
	} else {
		var target crypto.NodeID
		copy(target[:], targetBytes)
	}
	rq := dhtReqKey{info.key, target}
	sendPing := func() {
		a.core.dht.addCallback(&rq, func(res *dhtRes) {
			defer func() { recover() }()
			select {
			case resCh <- res:
			default:
			}
		})
		a.core.dht.ping(&info, &target)
	}
	a.core.router.doAdmin(sendPing)
	go func() {
		time.Sleep(6 * time.Second)
		close(resCh)
	}()
	for res := range resCh {
		return *res, nil
	}
	return dhtRes{}, errors.New(fmt.Sprintf("DHT ping timeout: %s", keyString))
}

func (a *AdminSocket) admin_getNodeInfo(keyString, coordString string, nocache bool) (nodeinfoPayload, error) {
	var key crypto.BoxPubKey
	if keyBytes, err := hex.DecodeString(keyString); err != nil {
		return nodeinfoPayload{}, err
	} else {
		copy(key[:], keyBytes)
	}
	if !nocache {
		if response, err := a.core.router.nodeinfo.getCachedNodeInfo(key); err == nil {
			return response, nil
		}
	}
	var coords []byte
	for _, cstr := range strings.Split(strings.Trim(coordString, "[]"), " ") {
		if cstr == "" {
			// Special case, happens if trimmed is the empty string, e.g. this is the root
			continue
		}
		if u64, err := strconv.ParseUint(cstr, 10, 8); err != nil {
			return nodeinfoPayload{}, err
		} else {
			coords = append(coords, uint8(u64))
		}
	}
	response := make(chan *nodeinfoPayload, 1)
	sendNodeInfoRequest := func() {
		a.core.router.nodeinfo.addCallback(key, func(nodeinfo *nodeinfoPayload) {
			defer func() { recover() }()
			select {
			case response <- nodeinfo:
			default:
			}
		})
		a.core.router.nodeinfo.sendNodeInfo(key, coords, false)
	}
	a.core.router.doAdmin(sendNodeInfoRequest)
	go func() {
		time.Sleep(6 * time.Second)
		close(response)
	}()
	for res := range response {
		return *res, nil
	}
	return nodeinfoPayload{}, errors.New(fmt.Sprintf("getNodeInfo timeout: %s", keyString))
}

// getResponse_dot returns a response for a graphviz dot formatted representation of the known parts of the network.
// This is color-coded and labeled, and includes the self node, switch peers, nodes known to the DHT, and nodes with open sessions.
// The graph is structured as a tree with directed links leading away from the root.
func (a *AdminSocket) getResponse_dot() []byte {
	self := a.getData_getSelf()
	peers := a.getData_getSwitchPeers()
	dht := a.getData_getDHT()
	sessions := a.getData_getSessions()
	// Start building a tree from all known nodes
	type nodeInfo struct {
		name    string
		key     string
		parent  string
		port    switchPort
		options string
	}
	infos := make(map[string]nodeInfo)
	// Get coords as a slice of strings, FIXME? this looks very fragile
	coordSlice := func(coords string) []string {
		tmp := strings.Replace(coords, "[", "", -1)
		tmp = strings.Replace(tmp, "]", "", -1)
		return strings.Split(tmp, " ")
	}
	// First fill the tree with all known nodes, no parents
	addInfo := func(nodes []admin_nodeInfo, options string, tag string) {
		for _, node := range nodes {
			n := node.asMap()
			info := nodeInfo{
				key:     n["coords"].(string),
				options: options,
			}
			if len(tag) > 0 {
				info.name = fmt.Sprintf("%s\n%s", n["ip"].(string), tag)
			} else {
				info.name = n["ip"].(string)
			}
			coordsSplit := coordSlice(info.key)
			if len(coordsSplit) != 0 {
				portStr := coordsSplit[len(coordsSplit)-1]
				portUint, err := strconv.ParseUint(portStr, 10, 64)
				if err == nil {
					info.port = switchPort(portUint)
				}
			}
			infos[info.key] = info
		}
	}
	addInfo(dht, "fillcolor=\"#ffffff\" style=filled fontname=\"sans serif\"", "Known in DHT")                               // white
	addInfo(sessions, "fillcolor=\"#acf3fd\" style=filled fontname=\"sans serif\"", "Open session")                          // blue
	addInfo(peers, "fillcolor=\"#ffffb5\" style=filled fontname=\"sans serif\"", "Connected peer")                           // yellow
	addInfo(append([]admin_nodeInfo(nil), *self), "fillcolor=\"#a5ff8a\" style=filled fontname=\"sans serif\"", "This node") // green
	// Now go through and create placeholders for any missing nodes
	for _, info := range infos {
		// This is ugly string manipulation
		coordsSplit := coordSlice(info.key)
		for idx := range coordsSplit {
			key := fmt.Sprintf("[%v]", strings.Join(coordsSplit[:idx], " "))
			newInfo, isIn := infos[key]
			if isIn {
				continue
			}
			newInfo.name = "?"
			newInfo.key = key
			newInfo.options = "fontname=\"sans serif\" style=dashed color=\"#999999\" fontcolor=\"#999999\""

			coordsSplit := coordSlice(newInfo.key)
			if len(coordsSplit) != 0 {
				portStr := coordsSplit[len(coordsSplit)-1]
				portUint, err := strconv.ParseUint(portStr, 10, 64)
				if err == nil {
					newInfo.port = switchPort(portUint)
				}
			}

			infos[key] = newInfo
		}
	}
	// Now go through and attach parents
	for _, info := range infos {
		pSplit := coordSlice(info.key)
		if len(pSplit) > 0 {
			pSplit = pSplit[:len(pSplit)-1]
		}
		info.parent = fmt.Sprintf("[%v]", strings.Join(pSplit, " "))
		infos[info.key] = info
	}
	// Finally, get a sorted list of keys, which we use to organize the output
	var keys []string
	for _, info := range infos {
		keys = append(keys, info.key)
	}
	// sort
	sort.SliceStable(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	sort.SliceStable(keys, func(i, j int) bool {
		return infos[keys[i]].port < infos[keys[j]].port
	})
	// Now print it all out
	var out []byte
	put := func(s string) {
		out = append(out, []byte(s)...)
	}
	put("digraph {\n")
	// First set the labels
	for _, key := range keys {
		info := infos[key]
		put(fmt.Sprintf("\"%v\" [ label = \"%v\" %v ];\n", info.key, info.name, info.options))
	}
	// Then print the tree structure
	for _, key := range keys {
		info := infos[key]
		if info.key == info.parent {
			continue
		} // happens for the root, skip it
		port := fmt.Sprint(info.port)
		style := "fontname=\"sans serif\""
		if infos[info.parent].name == "?" || infos[info.key].name == "?" {
			style = "fontname=\"sans serif\" style=dashed color=\"#999999\" fontcolor=\"#999999\""
		}
		put(fmt.Sprintf("  \"%+v\" -> \"%+v\" [ label = \"%v\" %s ];\n", info.parent, info.key, port, style))
	}
	put("}\n")
	return out
}
*/
