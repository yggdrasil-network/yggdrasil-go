package yggdrasil

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"yggdrasil/defaults"
)

// TODO: Add authentication

type admin struct {
	core       *Core
	listenaddr string
	listener   net.Listener
	handlers   []admin_handlerInfo
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
func (a *admin) addHandler(name string, args []string, handler func(admin_info) (admin_info, error)) {
	a.handlers = append(a.handlers, admin_handlerInfo{name, args, handler})
}

// init runs the initial admin setup.
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
	a.addHandler("getSwitchQueues", []string{}, func(in admin_info) (admin_info, error) {
		queues := a.getData_getSwitchQueues()
		return admin_info{"switchqueues": queues.asMap()}, nil
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
	a.addHandler("addPeer", []string{"uri", "[interface]"}, func(in admin_info) (admin_info, error) {
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
				a.core.tun.iface.Name(): admin_info{
					"tap_mode": a.core.tun.iface.IsTAP(),
					"mtu":      ifmtu,
				},
			}, nil
		}
	})
	a.addHandler("getMulticastInterfaces", []string{}, func(in admin_info) (admin_info, error) {
		var intfs []string
		for _, v := range a.core.multicast.interfaces() {
			intfs = append(intfs, v.Name)
		}
		return admin_info{"multicast_interfaces": intfs}, nil
	})
	a.addHandler("getAllowedEncryptionPublicKeys", []string{}, func(in admin_info) (admin_info, error) {
		return admin_info{"allowed_box_pubs": a.getAllowedEncryptionPublicKeys()}, nil
	})
	a.addHandler("addAllowedEncryptionPublicKey", []string{"key"}, func(in admin_info) (admin_info, error) {
		if a.addAllowedEncryptionPublicKey(in["key"].(string)) == nil {
			return admin_info{
				"added": []string{
					in["key"].(string),
				},
			}, nil
		} else {
			return admin_info{
				"not_added": []string{
					in["key"].(string),
				},
			}, errors.New("Failed to add allowed key")
		}
	})
	a.addHandler("removeAllowedEncryptionPublicKey", []string{"key"}, func(in admin_info) (admin_info, error) {
		if a.removeAllowedEncryptionPublicKey(in["key"].(string)) == nil {
			return admin_info{
				"removed": []string{
					in["key"].(string),
				},
			}, nil
		} else {
			return admin_info{
				"not_removed": []string{
					in["key"].(string),
				},
			}, errors.New("Failed to remove allowed key")
		}
	})
}

// start runs the admin API socket to listen for / respond to admin API calls.
func (a *admin) start() error {
	go a.listen()
	return nil
}

// cleans up when stopping
func (a *admin) close() error {
	return a.listener.Close()
}

// listen is run by start and manages API connections.
func (a *admin) listen() {
	u, err := url.Parse(a.listenaddr)
	if err == nil {
		switch strings.ToLower(u.Scheme) {
		case "unix":
			a.listener, err = net.Listen("unix", a.listenaddr[7:])
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
		a.core.log.Printf("Admin socket failed to listen: %v", err)
		os.Exit(1)
	}
	a.core.log.Printf("%s admin socket listening on %s",
		strings.ToUpper(a.listener.Addr().Network()),
		a.listener.Addr().String())
	defer a.listener.Close()
	for {
		conn, err := a.listener.Accept()
		if err == nil {
			a.handleRequest(conn)
		}
	}
}

// handleRequest calls the request handler for each request sent to the admin API.
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
	return fmt.Sprint(*n)
}

// printInfos returns a newline separated list of strings from admin_nodeInfos, e.g. a printable string of info about all peers.
func (a *admin) printInfos(infos []admin_nodeInfo) string {
	var out []string
	for _, info := range infos {
		out = append(out, info.toString())
	}
	out = append(out, "") // To add a trailing "\n" in the join
	return strings.Join(out, "\n")
}

// addPeer triggers a connection attempt to a node.
func (a *admin) addPeer(addr string, sintf string) error {
	u, err := url.Parse(addr)
	if err == nil {
		switch strings.ToLower(u.Scheme) {
		case "tcp":
			a.core.tcp.connect(u.Host, sintf)
		case "socks":
			a.core.tcp.connectSOCKS(u.Host, u.Path[1:])
		default:
			return errors.New("invalid peer: " + addr)
		}
	} else {
		// no url scheme provided
		addr = strings.ToLower(addr)
		if strings.HasPrefix(addr, "tcp:") {
			addr = addr[4:]
		}
		a.core.tcp.connect(addr, "")
		return nil
	}
	return nil
}

// removePeer disconnects an existing node (given by the node's port number).
func (a *admin) removePeer(p string) error {
	iport, err := strconv.Atoi(p)
	if err != nil {
		return err
	}
	a.core.peers.removePeer(switchPort(iport))
	return nil
}

// startTunWithMTU creates the tun/tap device, sets its address, and sets the MTU to the provided value.
func (a *admin) startTunWithMTU(ifname string, iftapmode bool, ifmtu int) error {
	// Close the TUN first if open
	_ = a.core.tun.close()
	// Then reconfigure and start it
	addr := a.core.router.addr
	straddr := fmt.Sprintf("%s/%v", net.IP(addr[:]).String(), 8*len(address_prefix)-1)
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

// getData_getSelf returns the self node's info for admin responses.
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

// getData_getPeers returns info from Core.peers for an admin response.
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

// getData_getSwitchPeers returns info from Core.switchTable for an admin response.
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
			{"bytes_sent", atomic.LoadUint64(&peer.bytesSent)},
			{"bytes_recvd", atomic.LoadUint64(&peer.bytesRecvd)},
		}
		peerInfos = append(peerInfos, info)
	}
	return peerInfos
}

// getData_getSwitchQueues returns info from Core.switchTable for an queue data.
func (a *admin) getData_getSwitchQueues() admin_nodeInfo {
	var peerInfos admin_nodeInfo
	switchTable := a.core.switchTable
	getSwitchQueues := func() {
		queues := make([]map[string]interface{}, 0)
		for k, v := range switchTable.queues.bufs {
			queue := map[string]interface{}{
				"queue_id":      k,
				"queue_size":    v.size,
				"queue_packets": len(v.packets),
			}
			queues = append(queues, queue)
		}
		peerInfos = admin_nodeInfo{
			{"queues", queues},
			{"queues_count", len(switchTable.queues.bufs)},
			{"queues_size", switchTable.queues.size},
			{"max_queues_count", switchTable.queues.maxbufs},
			{"max_queues_size", switchTable.queues.maxsize},
		}
	}
	a.core.switchTable.doAdmin(getSwitchQueues)
	return peerInfos
}

// getData_getDHT returns info from Core.dht for an admin response.
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

// getData_getSessions returns info from Core.sessions for an admin response.
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

// getAllowedEncryptionPublicKeys returns the public keys permitted for incoming peer connections.
func (a *admin) getAllowedEncryptionPublicKeys() []string {
	pubs := a.core.peers.getAllowedEncryptionPublicKeys()
	var out []string
	for _, pub := range pubs {
		out = append(out, hex.EncodeToString(pub[:]))
	}
	return out
}

// addAllowedEncryptionPublicKey whitelists a key for incoming peer connections.
func (a *admin) addAllowedEncryptionPublicKey(bstr string) (err error) {
	boxBytes, err := hex.DecodeString(bstr)
	if err == nil {
		var box boxPubKey
		copy(box[:], boxBytes)
		a.core.peers.addAllowedEncryptionPublicKey(&box)
	}
	return
}

// removeAllowedEncryptionPublicKey removes a key from the whitelist for incoming peer connections.
// If none are set, an empty list permits all incoming connections.
func (a *admin) removeAllowedEncryptionPublicKey(bstr string) (err error) {
	boxBytes, err := hex.DecodeString(bstr)
	if err == nil {
		var box boxPubKey
		copy(box[:], boxBytes)
		a.core.peers.removeAllowedEncryptionPublicKey(&box)
	}
	return
}

// getResponse_dot returns a response for a graphviz dot formatted representation of the known parts of the network.
// This is color-coded and labeled, and includes the self node, switch peers, nodes known to the DHT, and nodes with open sessions.
// The graph is structured as a tree with directed links leading away from the root.
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
