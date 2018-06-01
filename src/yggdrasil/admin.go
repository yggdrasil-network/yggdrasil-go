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
		handlers := make(map[string]interface{})
		for _, handler := range a.handlers {
			handlers[handler.name] = admin_info{"fields": handler.args}
		}
		return admin_info{"help": handlers}, nil
	})
	a.addHandler("dot", []string{}, func(in admin_info) (admin_info, error) {
		return admin_info{"dot": string(a.getResponse_dot())}, nil
	})
	a.addHandler("getSelf", []string{}, func(in admin_info) (admin_info, error) {
		self := a.getData_getSelf().asMap()
		ip := fmt.Sprint(self["ip"])
		delete(self, "ip")
		return admin_info{"self": admin_info{ip: self}}, nil
	})
	a.addHandler("getPeers", []string{}, func(in admin_info) (admin_info, error) {
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
	a.addHandler("getSwitchPeers", []string{}, func(in admin_info) (admin_info, error) {
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
	a.addHandler("getDHT", []string{}, func(in admin_info) (admin_info, error) {
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
	a.addHandler("getSessions", []string{}, func(in admin_info) (admin_info, error) {
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
			recover()
			r = admin_info{"none": admin_info{}}
			e = nil
		}()

		return admin_info{
			a.core.tun.iface.Name(): admin_info{
				"tap_mode": a.core.tun.iface.IsTAP(),
				"mtu":      a.core.tun.mtu,
			},
		}, nil
	})
	a.addHandler("setTunTap", []string{"name", "[tap_mode]", "[mtu]"}, func(in admin_info) (admin_info, error) {
		// Set sane defaults
		iftapmode := getDefaults().defaultIfTAPMode
		ifmtu := getDefaults().defaultIfMTU
		// Has TAP mode been specified?
		if tap, ok := in["tap_mode"]; ok {
			iftapmode = tap.(bool)
		}
		// Check we have enough params for MTU
		if mtu, ok := in["mtu"]; ok {
			if mtu.(float64) >= 1280 && ifmtu <= getDefaults().maximumIfMTU {
				ifmtu = int(in["mtu"].(float64))
			}
		}
		// Start the TUN adapter
		if err := a.startTunWithMTU(in["name"].(string), iftapmode, ifmtu); err != nil {
			return admin_info{}, errors.New("Failed to configure adapter")
		} else {
			return admin_info{
				a.core.tun.iface.Name(): admin_info{
					"tap_mode": a.core.tun.iface.IsTAP(),
					"mtu":      ifmtu,
				},
			}, nil
		}
	})
	a.addHandler("getMulticastInterfaces", []string{}, func(in admin_info) (admin_info, error) {
		var intfs []string
		for _, v := range a.core.multicast.interfaces {
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
			}, errors.New("Failed to add allowed box pub key")
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
			}, errors.New("Failed to remove allowed box pub key")
		}
	})
}

func (a *admin) start() error {
	go a.listen()
	return nil
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
			fmt.Println("Admin socket error:", r)
			if err := encoder.Encode(&send); err != nil {
				fmt.Println("Admin socket JSON encode error:", err)
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
			if recv["request"] == handler.name {
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
			//	fmt.Println("Admin socket JSON encode error:", err)
			return
		}

		// If "keepalive" isn't true then close the connection
		if keepalive, ok := recv["keepalive"]; !ok || !keepalive.(bool) {
			conn.Close()
		}
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
			a.core.tcp.connect(u.Host)
		case "udp":
			a.core.udp.connect(u.Host)
		case "socks":
			a.core.tcp.connectSOCKS(u.Host, u.Path[1:])
		default:
			return errors.New("invalid peer: " + addr)
		}
	} else {
		// no url scheme provided
		addr = strings.ToLower(addr)
		if strings.HasPrefix(addr, "udp:") {
			a.core.udp.connect(addr[4:])
			return nil
		} else {
			if strings.HasPrefix(addr, "tcp:") {
				addr = addr[4:]
			}
			a.core.tcp.connect(addr)
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
	coords := table.self.getCoords()
	self := admin_nodeInfo{
		{"ip", a.core.GetAddress().String()},
		{"subnet", a.core.GetSubnet().String()},
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
			{"uptime", int(time.Since(p.firstSeen).Seconds())},
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
						{"last_seen", int(now.Sub(v.recv).Seconds())},
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

func (a *admin) getAllowedEncryptionPublicKeys() []string {
	pubs := a.core.peers.getAllowedEncryptionPublicKeys()
	var out []string
	for _, pub := range pubs {
		out = append(out, hex.EncodeToString(pub[:]))
	}
	return out
}

func (a *admin) addAllowedEncryptionPublicKey(bstr string) (err error) {
	boxBytes, err := hex.DecodeString(bstr)
	if err == nil {
		var box boxPubKey
		copy(box[:], boxBytes)
		a.core.peers.addAllowedEncryptionPublicKey(&box)
	}
	return
}

func (a *admin) removeAllowedEncryptionPublicKey(bstr string) (err error) {
	boxBytes, err := hex.DecodeString(bstr)
	if err == nil {
		var box boxPubKey
		copy(box[:], boxBytes)
		a.core.peers.removeAllowedEncryptionPublicKey(&box)
	}
	return
}

func (a *admin) getResponse_dot() []byte {
	self := a.getData_getSelf()
	peers := a.getData_getSwitchPeers()
	dht := a.getData_getDHT()
	sessions := a.getData_getSessions()
	// Start building a tree from all known nodes
	type nodeInfo struct {
		name    string
		key     string
		parent  string
		options string
	}
	infos := make(map[string]nodeInfo)
	// First fill the tree with all known nodes, no parents
	addInfo := func(nodes []admin_nodeInfo, options string) {
		for _, node := range nodes {
			n := node.asMap()
			info := nodeInfo{
				name:    n["ip"].(string),
				key:     n["coords"].(string),
				options: options,
			}
			infos[info.key] = info
		}
	}
	addInfo(sessions, "fillcolor=indianred style=filled")
	addInfo(dht, "fillcolor=lightblue style=filled")
	addInfo(peers, "fillcolor=palegreen style=filled")
	addInfo(append([]admin_nodeInfo(nil), *self), "")
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
			newInfo.options = "style=filled"
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
		put(fmt.Sprintf("\"%v\" [ label = \"%v\" %v ];\n", info.key, info.name, info.options))
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
