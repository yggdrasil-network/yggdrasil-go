package types

import "net"

func connToChan(mtu uint64, conn net.Conn) chan []byte {
	c := make(chan []byte)
	go func() {
		for {
			b := make([]byte, mtu)
			n, err := conn.Read(b[:])
			if err != nil {
				c <- nil
				return
			}
			if n > 0 {
				c <- b[:n]
			}
		}
	}()
	return c
}

func ProxyTCP(mtu uint64, c1, c2 net.Conn) {
	p1, p2 := connToChan(mtu, c1), connToChan(mtu, c2)
	defer c1.Close()
	defer c2.Close()
	for {
		select {
		case b := <-p1:
			if b == nil {
				return
			} else if _, err := c2.Write(b); err != nil {
				return
			}
		case b := <-p2:
			if b == nil {
				return
			} else if _, err := c1.Write(b); err != nil {
				return
			}
		}
	}
}
