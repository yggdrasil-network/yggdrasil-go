package yggdrasil

import "net"
import "os"
import "bytes"
import "fmt"

// TODO: Make all of this JSON

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
	case "ports":
		ports := a.core.peers.getPorts()
		for _, v := range ports {
			conn.Write([]byte(fmt.Sprintf("Found switch port %d\n", v.port)))
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
