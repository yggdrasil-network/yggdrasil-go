package yggdrasil

import "net"
import "os"
import "encoding/hex"
import "encoding/json"
import "errors"
import "fmt"
import "net/url"
import "sort"
import "strings"
import "strconv"
import "sync/atomic"
import "time"

// TODO: Add authentication

type admin struct {
	core       *Core
	listenaddr string
	handlers   []admin_handlerInfo
}

type admin_info map[string]interface{}

type admin_handlerInfo struct {
	name    string                               // Checked against the first word of the api call
	args    []string                             // List of human-readable argument names
	handler func(admin_info) (admin_info, error) // First is input map, second is output
}

// Maps things like "IP", "port", "bucket", or "coords" onto strings
type admin_pair struct {
	key string
	val interface{}
}
type admin_nodeInfo []admin_pair

func (a *admin) addHandler(name string, args []string, handler func(admin_info) (admin_info, error)) {
	a.handlers = append(a.handlers, admin_handlerInfo{name, args, handler})
}

func (a *admin) init(c *Core, listenaddr string) {
	a.core = c
	a.listenaddr = listenaddr
	a.addHandler("help", nil, func(in admin_info) (admin_info, error) {
		handlers := make(map[string][]string)
		for _, handler := range a.handlers {
			handlers[handler.name] = handler.args
		}
		return admin_info{"handlers": handlers}, nil
	})
	a.addHandler("dot", nil, func(in admin_info) (admin_info, error) {
		return admin_info{"dot": string(a.getResponse_dot())}, nil
	})
	a.addHandler("getSelf", nil, func(in admin_info) (admin_info, error) {
		return admin_info{"self": a.getData_getSelf().asMap()}, nil
	})
	a.addHandler("getPeers", nil, func(in admin_info) (admin_info, error) {
		sort := "ip"
		peers := make(admin_info)
		for _, peerdata := range a.getData_getPeers() {
			p := peerdata.asMap()
			so := fmt.Sprint(p[sort])
			peers[so] = p
			delete(peers[so].(map[string]interface{}), sort)
		}
		return admin_info{"peers": peers}, nil
	})
	a.addHandler("getSwitchPeers", nil, func(in admin_info) (admin_info, error) {
		sort := "port"
		switchpeers := make(admin_info)
		for _, s := range a.getData_getSwitchPeers() {
			p := s.asMap()
			so := fmt.Sprint(p[sort])
			switchpeers[so] = p
			delete(switchpeers[so].(map[string]interface{}), sort)
		}
		return admin_info{"switchpeers": switchpeers}, nil
	})
	a.addHandler("getDHT", nil, func(in admin_info) (admin_info, error) {
		sort := "ip"
		dht := make(admin_info)
		for _, d := range a.getData_getDHT() {
			p := d.asMap()
			so := fmt.Sprint(p[sort])
			dht[so] = p
			delete(dht[so].(map[string]interface{}), sort)
		}
		return admin_info{"dht": dht}, nil
	})
	a.addHandler("getSessions", nil, func(in admin_info) (admin_info, error) {
		sort := "ip"
		sessions := make(admin_info)
		for _, s := range a.getData_getSessions() {
			p := s.asMap()
			so := fmt.Sprint(p[sort])
			sessions[so] = p
			delete(sessions[so].(map[string]interface{}), sort)
		}
		return admin_info{"sessions": sessions}, nil
	})
	a.addHandler("addPeer", []string{"uri"}, func(in admin_info) (admin_info, error) {
		if a.addPeer(in["uri"].(string)) == nil {
			return admin_info{
				"peers_added": []string{
					in["uri"].(string),
				},
			}, nil
		} else {
			return admin_info{
				"peers_not_added": []string{
					in["uri"].(string),
				},
			}, errors.New("Failed to add peer")
		}
	})
	a.addHandler("removePeer", []string{"port"}, func(in admin_info) (admin_info, error) {
		if a.removePeer(fmt.Sprint(in["port"])) == nil {
			return admin_info{
				"peers_removed": []string{
					fmt.Sprint(in["port"]),
				},
			}, nil
		} else {
			return admin_info{
				"peers_not_removed": []string{
					fmt.Sprint(in["port"]),
				},
			}, errors.New("Failed to remove peer")
		}
	})
	a.addHandler("getTunTap", nil, func(in admin_info) (r admin_info, e error) {
		defer func() {
			recover()
			r = admin_info{"name": "none"}
			e = nil
		}()

		return admin_info{
			"name":     a.core.tun.iface.Name(),
			"tap_mode": a.core.tun.iface.IsTAP(),
			"mtu":      a.core.tun.mtu,
		}, nil
	})
	/*
		a.addHandler("setTunTap", []string{"<ifname|auto|none>", "[<tun|tap>]", "[<mtu>]"}, func(out *[]byte, ifparams ...string) {
			// Set sane defaults
			iftapmode := false
			ifmtu := 1280
			var err error
			// Check we have enough params for TAP mode
			if len(ifparams) > 1 {
				// Is it a TAP adapter?
				if ifparams[1] == "tap" {
					iftapmode = true
				}
			}
			// Check we have enough params for MTU
			if len(ifparams) > 2 {
				// Make sure the MTU is sane
				ifmtu, err = strconv.Atoi(ifparams[2])
				if err != nil || ifmtu < 1280 || ifmtu > 65535 {
					ifmtu = 1280
				}
			}
			// Start the TUN adapter
			if err := a.startTunWithMTU(ifparams[0], iftapmode, ifmtu); err != nil {
				*out = []byte(fmt.Sprintf("Failed to set TUN: %v\n", err))
			} else {
				info := admin_nodeInfo{
					{"Interface name", ifparams[0]},
					{"TAP mode", strconv.FormatBool(iftapmode)},
					{"mtu", strconv.Itoa(ifmtu)},
				}
				*out = []byte(a.printInfos([]admin_nodeInfo{info}))
			}
		})
	*/
	/*
		a.addHandler("getAllowedBoxPubs", nil, func(out *[]byte, _ ...string) {
			*out = []byte(a.getAllowedBoxPubs())
		})
		a.addHandler("addAllowedBoxPub", []string{"<boxPubKey>"}, func(out *[]byte, saddr ...string) {
			if a.addAllowedBoxPub(saddr[0]) == nil {
				*out = []byte("Adding key: " + saddr[0] + "\n")
			} else {
				*out = []byte("Failed to add key: " + saddr[0] + "\n")
			}
		})
		a.addHandler("removeAllowedBoxPub", []string{"<boxPubKey>"}, func(out *[]byte, sport ...string) {
			if a.removeAllowedBoxPub(sport[0]) == nil {
				*out = []byte("Removing key: " + sport[0] + "\n")
			} else {
				*out = []byte("Failed to remove key: " + sport[0] + "\n")
			}
		})*/
	go a.listen()
}

func (a *admin) listen() {
	l, err := net.Listen("tcp", a.listenaddr)
	if err != nil {
		a.core.log.Printf("Admin socket failed to listen: %v", err)
		os.Exit(1)
	}
	defer l.Close()
	a.core.log.Printf("Admin socket listening on %s", l.Addr().String())
	for {
		conn, err := l.Accept()
		if err == nil {
			a.handleRequest(conn)
		}
	}
}

func (a *admin) handleRequest(conn net.Conn) {
	defer conn.Close()
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	encoder.SetIndent("", "  ")
	recv := make(admin_info)
	send := make(admin_info)

	for {
		if err := decoder.Decode(&recv); err != nil {
			fmt.Println("Admin socket JSON decode error:", err)
			return
		}

		send["request"] = recv
		send["status"] = "error"

	handlers:
		for _, handler := range a.handlers {
			if recv["request"] == handler.name {
				// Check that we have all the required arguments
				for _, arg := range handler.args {
					// An argument in <pointy brackets> is optional and not required,
					// so we can safely ignore those
					if strings.HasPrefix(arg, "<") && strings.HasSuffix(arg, ">") {
						continue
					}
					// Check if the field is missing
					if _, ok := recv[arg]; !ok {
						fmt.Println("Missing required argument", arg)
						send = admin_info{
							"error":     "One or more expected fields missing",
							"expecting": handler.args,
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

		if err := encoder.Encode(&send); err != nil {
			fmt.Println("Admin socket JSON encode error:", err)
			return
		}

		return
	}
}

func (n *admin_nodeInfo) asMap() map[string]interface{} {
	m := make(map[string]interface{}, len(*n))
	for _, p := range *n {
		m[p.key] = p.val
	}
	return m
}

func (n *admin_nodeInfo) toString() string {
	// TODO return something nicer looking than this
	var out []string
	for _, p := range *n {
		out = append(out, fmt.Sprintf("%v: %v", p.key, p.val))
	}
	return strings.Join(out, ", ")
	return fmt.Sprint(*n)
}

func (a *admin) printInfos(infos []admin_nodeInfo) string {
	var out []string
	for _, info := range infos {
		out = append(out, info.toString())
	}
	out = append(out, "") // To add a trailing "\n" in the join
	return strings.Join(out, "\n")
}

func (a *admin) addPeer(addr string) error {
	u, err := url.Parse(addr)
	if err == nil {
		switch strings.ToLower(u.Scheme) {
		case "tcp":
			a.core.DEBUG_addTCPConn(u.Host)
		case "udp":
			a.core.DEBUG_maybeSendUDPKeys(u.Host)
		case "socks":
			a.core.DEBUG_addSOCKSConn(u.Host, u.Path[1:])
		default:
			return errors.New("invalid peer: " + addr)
		}
	} else {
		// no url scheme provided
		addr = strings.ToLower(addr)
		if strings.HasPrefix(addr, "udp:") {
			a.core.DEBUG_maybeSendUDPKeys(addr[4:])
			return nil
		} else {
			if strings.HasPrefix(addr, "tcp:") {
				addr = addr[4:]
			}
			a.core.DEBUG_addTCPConn(addr)
			return nil
		}
		return errors.New("invalid peer: " + addr)
	}
	return nil
}

func (a *admin) removePeer(p string) error {
	iport, err := strconv.Atoi(p)
	if err != nil {
		return err
	}
	a.core.peers.removePeer(switchPort(iport))
	return nil
}

func (a *admin) startTunWithMTU(ifname string, iftapmode bool, ifmtu int) error {
	// Close the TUN first if open
	_ = a.core.tun.close()
	// Then reconfigure and start it
	addr := a.core.router.addr
	straddr := fmt.Sprintf("%s/%v", net.IP(addr[:]).String(), 8*len(address_prefix))
	if ifname != "none" {
		err := a.core.tun.setup(ifname, iftapmode, straddr, ifmtu)
		if err != nil {
			return err
		}
		// If we have open sessions then we need to notify them
		// that our MTU has now changed
		for _, sinfo := range a.core.sessions.sinfos {
			if ifname == "none" {
				sinfo.myMTU = 0
			} else {
				sinfo.myMTU = uint16(ifmtu)
			}
			a.core.sessions.sendPingPong(sinfo, false)
		}
		// Aaaaand... go!
		go a.core.tun.read()
	}
	go a.core.tun.write()
	return nil
}

func (a *admin) getData_getSelf() *admin_nodeInfo {
	table := a.core.switchTable.table.Load().(lookupTable)
	addr := a.core.router.addr
	coords := table.self.getCoords()
	self := admin_nodeInfo{
		{"ip", net.IP(addr[:]).String()},
		{"coords", fmt.Sprint(coords)},
	}
	return &self
}

func (a *admin) getData_getPeers() []admin_nodeInfo {
	ports := a.core.peers.ports.Load().(map[switchPort]*peer)
	var peerInfos []admin_nodeInfo
	var ps []switchPort
	for port := range ports {
		ps = append(ps, port)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i] < ps[j] })
	for _, port := range ps {
		p := ports[port]
		addr := *address_addrForNodeID(getNodeID(&p.box))
		info := admin_nodeInfo{
			{"ip", net.IP(addr[:]).String()},
			{"port", port},
			{"uptime", fmt.Sprint(time.Since(p.firstSeen))},
			{"bytes_sent", atomic.LoadUint64(&p.bytesSent)},
			{"bytes_recvd", atomic.LoadUint64(&p.bytesRecvd)},
		}
		peerInfos = append(peerInfos, info)
	}
	return peerInfos
}

func (a *admin) getData_getSwitchPeers() []admin_nodeInfo {
	var peerInfos []admin_nodeInfo
	table := a.core.switchTable.table.Load().(lookupTable)
	peers := a.core.peers.ports.Load().(map[switchPort]*peer)
	for _, elem := range table.elems {
		peer, isIn := peers[elem.port]
		if !isIn {
			continue
		}
		addr := *address_addrForNodeID(getNodeID(&peer.box))
		coords := elem.locator.getCoords()
		info := admin_nodeInfo{
			{"ip", net.IP(addr[:]).String()},
			{"coords", fmt.Sprint(coords)},
			{"port", elem.port},
		}
		peerInfos = append(peerInfos, info)
	}
	return peerInfos
}

func (a *admin) getData_getDHT() []admin_nodeInfo {
	var infos []admin_nodeInfo
	now := time.Now()
	getDHT := func() {
		for i := 0; i < a.core.dht.nBuckets(); i++ {
			b := a.core.dht.getBucket(i)
			getInfo := func(vs []*dhtInfo, isPeer bool) {
				for _, v := range vs {
					addr := *address_addrForNodeID(v.getNodeID())
					info := admin_nodeInfo{
						{"ip", net.IP(addr[:]).String()},
						{"coords", fmt.Sprint(v.coords)},
						{"bucket", i},
						{"peer_only", isPeer},
						{"last_seen", fmt.Sprint(now.Sub(v.recv))},
					}
					infos = append(infos, info)
				}
			}
			getInfo(b.other, false)
			getInfo(b.peers, true)
		}
	}
	a.core.router.doAdmin(getDHT)
	return infos
}

func (a *admin) getData_getSessions() []admin_nodeInfo {
	var infos []admin_nodeInfo
	getSessions := func() {
		for _, sinfo := range a.core.sessions.sinfos {
			// TODO? skipped known but timed out sessions?
			info := admin_nodeInfo{
				{"ip", net.IP(sinfo.theirAddr[:]).String()},
				{"coords", fmt.Sprint(sinfo.coords)},
				{"mtu", sinfo.getMTU()},
				{"was_mtu_fixed", sinfo.wasMTUFixed},
				{"bytes_sent", sinfo.bytesSent},
				{"bytes_recvd", sinfo.bytesRecvd},
			}
			infos = append(infos, info)
		}
	}
	a.core.router.doAdmin(getSessions)
	return infos
}

func (a *admin) getAllowedBoxPubs() string {
	pubs := a.core.peers.getAllowedBoxPubs()
	var out []string
	for _, pub := range pubs {
		out = append(out, hex.EncodeToString(pub[:]))
	}
	out = append(out, "")
	return strings.Join(out, "\n")
}

func (a *admin) addAllowedBoxPub(bstr string) (err error) {
	boxBytes, err := hex.DecodeString(bstr)
	if err == nil {
		var box boxPubKey
		copy(box[:], boxBytes)
		a.core.peers.addAllowedBoxPub(&box)
	}
	return
}

func (a *admin) removeAllowedBoxPub(bstr string) (err error) {
	boxBytes, err := hex.DecodeString(bstr)
	if err == nil {
		var box boxPubKey
		copy(box[:], boxBytes)
		a.core.peers.removeAllowedBoxPub(&box)
	}
	return
}

func (a *admin) getResponse_dot() []byte {
	self := a.getData_getSelf().asMap()
	myAddr := self["IP"]
	peers := a.getData_getSwitchPeers()
	dht := a.getData_getDHT()
	sessions := a.getData_getSessions()
	// Map of coords onto IP
	m := make(map[string]string)
	m[self["coords"].(string)] = self["ip"].(string)
	for _, peer := range peers {
		p := peer.asMap()
		m[p["coords"].(string)] = p["ip"].(string)
	}
	for _, node := range dht {
		n := node.asMap()
		m[n["coords"].(string)] = n["ip"].(string)
	}
	for _, node := range sessions {
		n := node.asMap()
		m[n["coords"].(string)] = n["ip"].(string)
	}

	// Start building a tree from all known nodes
	type nodeInfo struct {
		name   string
		key    string
		parent string
	}
	infos := make(map[string]nodeInfo)
	// First fill the tree with all known nodes, no parents
	for k, n := range m {
		infos[k] = nodeInfo{
			name: n,
			key:  k,
		}
	}
	// Get coords as a slice of strings, FIXME? this looks very fragile
	coordSlice := func(coords string) []string {
		tmp := strings.Replace(coords, "[", "", -1)
		tmp = strings.Replace(tmp, "]", "", -1)
		return strings.Split(tmp, " ")
	}
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
	// TODO sort
	less := func(i, j int) bool {
		return keys[i] < keys[j]
	}
	sort.Slice(keys, less)
	// Now print it all out
	var out []byte
	put := func(s string) {
		out = append(out, []byte(s)...)
	}
	put("digraph {\n")
	// First set the labels
	for _, key := range keys {
		info := infos[key]
		if info.name == myAddr {
			put(fmt.Sprintf("\"%v\" [ style = \"filled\", label = \"%v\" ];\n", info.key, info.name))
		} else {
			put(fmt.Sprintf("\"%v\" [ label = \"%v\" ];\n", info.key, info.name))
		}
	}
	// Then print the tree structure
	for _, key := range keys {
		info := infos[key]
		if info.key == info.parent {
			continue
		} // happens for the root, skip it
		coordsSplit := coordSlice(key)
		if len(coordsSplit) == 0 {
			continue
		}
		port := coordsSplit[len(coordsSplit)-1]
		put(fmt.Sprintf("  \"%+v\" -> \"%+v\" [ label = \"%v\" ];\n", info.parent, info.key, port))
	}
	put("}\n")
	return out
}
