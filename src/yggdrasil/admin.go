package yggdrasil

import "net"
import "os"
import "bytes"
import "encoding/hex"
import "errors"
import "fmt"
import "net/url"
import "sort"
import "strings"
import "strconv"

// TODO? Make all of this JSON
// TODO: Add authentication

type admin struct {
	core       *Core
	listenaddr string
	handlers   []admin_handlerInfo
}

type admin_handlerInfo struct {
	name    string                   // Checked against the first word of the api call
	args    []string                 // List of human-readable argument names
	handler func(*[]byte, ...string) // First arg is pointer to the out slice, rest is args
}

func (a *admin) addHandler(name string, args []string, handler func(*[]byte, ...string)) {
	a.handlers = append(a.handlers, admin_handlerInfo{name, args, handler})
}

func (a *admin) init(c *Core, listenaddr string) {
	a.core = c
	a.listenaddr = listenaddr
	a.addHandler("help", nil, func(out *[]byte, _ ...string) {
		for _, handler := range a.handlers {
			tmp := append([]string{handler.name}, handler.args...)
			*out = append(*out, []byte(strings.Join(tmp, " "))...)
			*out = append(*out, "\n"...)
		}
	})
	// TODO? have other parts of the program call to add their own handlers
	a.addHandler("dot", nil, func(out *[]byte, _ ...string) {
		*out = a.getResponse_dot()
	})
	a.addHandler("getSelf", nil, func(out *[]byte, _ ...string) {
		*out = []byte(a.printInfos([]admin_nodeInfo{*a.getData_getSelf()}))
	})
	a.addHandler("getPeers", nil, func(out *[]byte, _ ...string) {
		*out = []byte(a.printInfos(a.getData_getPeers()))
	})
	a.addHandler("getSwitchPeers", nil, func(out *[]byte, _ ...string) {
		*out = []byte(a.printInfos(a.getData_getSwitchPeers()))
	})
	a.addHandler("getDHT", nil, func(out *[]byte, _ ...string) {
		*out = []byte(a.printInfos(a.getData_getDHT()))
	})
	a.addHandler("getSessions", nil, func(out *[]byte, _ ...string) {
		*out = []byte(a.printInfos(a.getData_getSessions()))
	})
	a.addHandler("addPeer", []string{"<proto://address:port>"}, func(out *[]byte, saddr ...string) {
		if a.addPeer(saddr[0]) == nil {
			*out = []byte("Adding peer: " + saddr[0] + "\n")
		} else {
			*out = []byte("Failed to add peer: " + saddr[0] + "\n")
		}
	})
	a.addHandler("removePeer", []string{"<port>"}, func(out *[]byte, sport ...string) {
		if a.removePeer(sport[0]) == nil {
			*out = []byte("Removing peer: " + sport[0] + "\n")
		} else {
			*out = []byte("Failed to remove peer: " + sport[0] + "\n")
		}
	})
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
				{"MTU", strconv.Itoa(ifmtu)},
			}
			*out = []byte(a.printInfos([]admin_nodeInfo{info}))
		}
	})
	a.addHandler("getAuthBoxPubs", nil, func(out *[]byte, _ ...string) {
		*out = []byte(a.getAuthBoxPubs())
	})
	a.addHandler("addAuthBoxPub", []string{"<boxPubKey>"}, func(out *[]byte, saddr ...string) {
		if a.addAuthBoxPub(saddr[0]) == nil {
			*out = []byte("Adding key: " + saddr[0] + "\n")
		} else {
			*out = []byte("Failed to add key: " + saddr[0] + "\n")
		}
	})
	a.addHandler("removeAuthBoxPub", []string{"<boxPubKey>"}, func(out *[]byte, sport ...string) {
		if a.removeAuthBoxPub(sport[0]) == nil {
			*out = []byte("Removing key: " + sport[0] + "\n")
		} else {
			*out = []byte("Failed to remove key: " + sport[0] + "\n")
		}
	})
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
	buf := make([]byte, 1024)
	_, err := conn.Read(buf)
	if err != nil {
		a.core.log.Printf("Admin socket failed to read: %v", err)
		conn.Close()
		return
	}
	var out []byte
	buf = bytes.Trim(buf, "\x00\r\n\t")
	call := strings.Split(string(buf), " ")
	var cmd string
	var args []string
	if len(call) > 0 {
		cmd = call[0]
		args = call[1:]
	}
	done := false
	for _, handler := range a.handlers {
		if cmd == handler.name {
			handler.handler(&out, args...)
			done = true
			break
		}
	}
	if !done {
		out = []byte("I didn't understand that!\n")
	}
	_, err = conn.Write(out)
	if err != nil {
		a.core.log.Printf("Admin socket error: %v", err)
	}
	conn.Close()
}

// Maps things like "IP", "port", "bucket", or "coords" onto strings
type admin_pair struct {
	key string
	val string
}
type admin_nodeInfo []admin_pair

func (n *admin_nodeInfo) asMap() map[string]string {
	m := make(map[string]string, len(*n))
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
		{"IP", net.IP(addr[:]).String()},
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
			{"IP", net.IP(addr[:]).String()},
			{"port", fmt.Sprint(port)},
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
			{"IP", net.IP(addr[:]).String()},
			{"coords", fmt.Sprint(coords)},
			{"port", fmt.Sprint(elem.port)},
		}
		peerInfos = append(peerInfos, info)
	}
	return peerInfos
}

func (a *admin) getData_getDHT() []admin_nodeInfo {
	var infos []admin_nodeInfo
	getDHT := func() {
		for i := 0; i < a.core.dht.nBuckets(); i++ {
			b := a.core.dht.getBucket(i)
			for _, v := range b.other {
				addr := *address_addrForNodeID(v.getNodeID())
				info := admin_nodeInfo{
					{"IP", net.IP(addr[:]).String()},
					{"coords", fmt.Sprint(v.coords)},
					{"bucket", fmt.Sprint(i)},
				}
				infos = append(infos, info)
			}
			for _, v := range b.peers {
				addr := *address_addrForNodeID(v.getNodeID())
				info := admin_nodeInfo{
					{"IP", net.IP(addr[:]).String()},
					{"coords", fmt.Sprint(v.coords)},
					{"bucket", fmt.Sprint(i)},
				}
				infos = append(infos, info)
			}
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
				{"IP", net.IP(sinfo.theirAddr[:]).String()},
				{"coords", fmt.Sprint(sinfo.coords)},
				{"MTU", fmt.Sprint(sinfo.getMTU())},
			}
			infos = append(infos, info)
		}
	}
	a.core.router.doAdmin(getSessions)
	return infos
}

func (a *admin) getAuthBoxPubs() string {
	pubs := a.core.peers.getAuthBoxPubs()
	var out []string
	for _, pub := range pubs {
		out = append(out, hex.EncodeToString(pub[:]))
	}
	out = append(out, "")
	return strings.Join(out, "\n")
}

func (a *admin) addAuthBoxPub(bstr string) (err error) {
	boxBytes, err := hex.DecodeString(bstr)
	if err == nil {
		var box boxPubKey
		copy(box[:], boxBytes)
		a.core.peers.addAuthBoxPub(&box)
	}
	return
}

func (a *admin) removeAuthBoxPub(bstr string) (err error) {
	boxBytes, err := hex.DecodeString(bstr)
	if err == nil {
		var box boxPubKey
		copy(box[:], boxBytes)
		a.core.peers.removeAuthBoxPub(&box)
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
	m[self["coords"]] = self["IP"]
	for _, peer := range peers {
		p := peer.asMap()
		m[p["coords"]] = p["IP"]
	}
	for _, node := range dht {
		n := node.asMap()
		m[n["coords"]] = n["IP"]
	}
	for _, node := range sessions {
		n := node.asMap()
		m[n["coords"]] = n["IP"]
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
