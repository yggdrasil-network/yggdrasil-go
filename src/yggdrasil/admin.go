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
	case "switch table":
		table := a.core.switchTable.table.Load().(lookupTable)
		conn.Write([]byte(fmt.Sprintf(
      "port 0 -> %+v\n",
      table.self.coords)))
		for _, v := range table.elems {
			conn.Write([]byte(fmt.Sprintf(
        "port %d -> %+v\n",
				v.port,
				v.locator.coords)))
		}
		break

	case "dht":
		n := a.core.dht.nBuckets()
		for i := 0; i < n; i++ {
			b := a.core.dht.getBucket(i)
			if len(b.infos) == 0 {
				continue
			}
			for _, v := range b.infos {
				addr := address_addrForNodeID(v.nodeID_hidden)
				ip := net.IP(addr[:]).String()

				conn.Write([]byte(fmt.Sprintf("%+v -> %+v\n",
					ip,
					v.coords)))
			}
		}
		break

	default:
		conn.Write([]byte("I didn't understand that!\n"))
	}
	if err != nil {
		a.core.log.Printf("Admin socket error: %v", err)
	}
	conn.Close()
}
