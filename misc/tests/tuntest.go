package main

import (
	"log"
	"net"
	"sync"

	"github.com/FlexibleBroadband/tun-go"
)

// first start server tun server.
func main() {
	wg := sync.WaitGroup{}
	// local tun interface read and write channel.
	rCh := make(chan []byte, 1024)
	// read from local tun interface channel, and write into remote udp channel.
	wg.Add(1)
	go func() {
		wg.Done()
		for {
			data := <-rCh
			// if data[0]&0xf0 == 0x40
			// write into udp conn.
			log.Println("tun->conn:", len(data))
			log.Println("read!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
			log.Println("src:", net.IP(data[8:24]), "dst:", net.IP(data[24:40]))
		}
	}()

	address := net.ParseIP("fc00::1")
	tuntap, err := tun.OpenTun(address)
	if err != nil {
		panic(err)
	}
	defer tuntap.Close()
	// read data from tun into rCh channel.
	wg.Add(1)
	go func() {
		if err := tuntap.Read(rCh); err != nil {
			panic(err)
		}
		wg.Done()
	}()
	wg.Wait()
}
