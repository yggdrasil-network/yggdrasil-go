package yggdrasil

import "net"
import "os"
import "bytes"
import "fmt"

// TODO: Make all of this JSON
// TODO: Add authentication
// TODO: Is any of this thread safe?

type admin struct {
	core       *Core
	listenaddr string
}

func (a *admin) init(c *Core, listenaddr string) {
	a.core = c
	a.listenaddr = listenaddr
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
	buf = bytes.Trim(buf, "\x00\r\n\t")
	switch string(buf) {
	case "dot":
		const mDepth = 1024
		m := make(map[[mDepth]switchPort]string)
		table := a.core.switchTable.table.Load().(lookupTable)
		peers := a.core.peers.ports.Load().(map[switchPort]*peer)

		// Add my own entry
		peerID := address_addrForNodeID(getNodeID(&peers[0].box))
		myAddr := net.IP(peerID[:]).String()
		var index [mDepth]switchPort
		copy(index[:mDepth], table.self.coords[:])
		m[index] = myAddr

		// Connect switch table entries to peer entries
		for _, tableentry := range table.elems {
			for _, peerentry := range peers {
				if peerentry.port == tableentry.port {
					peerID := address_addrForNodeID(getNodeID(&peerentry.box))
					addr := net.IP(peerID[:]).String()
					var index [mDepth]switchPort
					copy(index[:], tableentry.locator.coords)
					m[index] = addr
				}
			}
		}

		getPorts := func(coords []byte) []switchPort {
			var ports []switchPort
			for offset := 0; ; {
				coord, length := wire_decode_uint64(coords[offset:])
				if length == 0 {
					break
				}
				ports = append(ports, switchPort(coord))
				offset += length
			}
			return ports
		}

		// Look up everything we know from DHT
		getDHT := func() {
			for i := 0; i < a.core.dht.nBuckets(); i++ {
				b := a.core.dht.getBucket(i)
				for _, v := range b.infos {
					destPorts := getPorts(v.coords)
					addr := net.IP(address_addrForNodeID(v.nodeID_hidden)[:]).String()
					var index [mDepth]switchPort
					copy(index[:], destPorts)
					m[index] = addr
				}
			}
		}
		a.core.router.doAdmin(getDHT)

		// Look up everything we know from active sessions
		getSessions := func() {
			for _, sinfo := range a.core.sessions.sinfos {
				destPorts := getPorts(sinfo.coords)
				var index [mDepth]switchPort
				copy(index[:], destPorts)
				m[index] = net.IP(sinfo.theirAddr[:]).String()
			}
		}
		a.core.router.doAdmin(getSessions)

		// Start building a tree from all known nodes
		type nodeInfo struct {
			name   string
			key    [mDepth]switchPort
			parent [mDepth]switchPort
		}
		infos := make(map[[mDepth]switchPort]nodeInfo)
		// First fill the tree with all known nodes, no parents
		for k, n := range m {
			infos[k] = nodeInfo{
				name: n,
				key:  k,
			}
		}
		// Now go through and create placeholders for any missing nodes
		for _, info := range infos {
			for idx, port := range info.key {
				if port == 0 {
					break
				}
				var key [mDepth]switchPort
				copy(key[:idx], info.key[:])
				newInfo, isIn := infos[key]
				if isIn {
					continue
				}
				newInfo.name = "missing"
				newInfo.key = key
				infos[key] = newInfo
			}
		}
		// Now go through and attach parents
		for _, info := range infos {
			info.parent = info.key
			for idx := len(info.parent) - 1; idx >= 0; idx-- {
				if info.parent[idx] != 0 {
					info.parent[idx] = 0
					break
				}
			}
			infos[info.key] = info
		}
		// Now print it all out
		conn.Write([]byte(fmt.Sprintf("digraph {\n")))
		// First set the labels
		for _, info := range infos {
			if info.name == myAddr {
				conn.Write([]byte(fmt.Sprintf("\"%v\" [ style = \"filled\", label = \"%v\" ];\n", info.key, info.name)))
			} else {
				conn.Write([]byte(fmt.Sprintf("\"%v\" [ label = \"%v\" ];\n", info.key, info.name)))
			}
		}
		// Then print the tree structure
		for _, info := range infos {
			if info.key == info.parent {
				continue
			} // happens for the root, skip it
			conn.Write([]byte(fmt.Sprintf("  \"%+v\" -> \"%+v\";\n", info.key, info.parent)))
		}
		conn.Write([]byte(fmt.Sprintf("}\n")))
		break

	default:
		conn.Write([]byte("I didn't understand that!\n"))
	}
	if err != nil {
		a.core.log.Printf("Admin socket error: %v", err)
	}
	conn.Close()
}
