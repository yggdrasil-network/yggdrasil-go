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
		addr := net.IP(peerID[:]).String()
		var index [mDepth]switchPort
		copy(index[:mDepth], table.self.coords[:])
		m[index] = addr

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

		// Now print it all out
		conn.Write([]byte(fmt.Sprintf("graph {\n")))
		for k := range m {
			var mask [mDepth]switchPort
			copy(mask[:mDepth], k[:])
			for mk := range mask {
				mask[len(mask)-1-mk] = 0
				if len(m[k]) == 0 {
					m[k] = fmt.Sprintf("%+v (missing)", k)
				}
				if len(m[mask]) == 0 {
					m[mask] = fmt.Sprintf("%+v (missing)", mask)
				}
				if len(m[mask]) > 0 && m[mask] != m[k] {
					conn.Write([]byte(fmt.Sprintf("  \"%+v\" -- \"%+v\";\n", m[k], m[mask])))
					break
				}
			}
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
