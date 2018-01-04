package main

import "fmt"
import "net"
import "time"

func main() {
	addr, err := net.ResolveTCPAddr("tcp", "[::1]:9001")
	if err != nil {
		panic(err)
	}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	packetSize := 65535
	numPackets := 65535

	go func() {
		send, err := net.DialTCP("tcp", nil, addr)
		if err != nil {
			panic(err)
		}
		defer send.Close()
		msg := make([]byte, packetSize)
		for idx := 0; idx < numPackets; idx++ {
			send.Write(msg)
		}
	}()

	start := time.Now()
	//msg := make([]byte, 1280)
	sock, err := listener.AcceptTCP()
	if err != nil {
		panic(err)
	}
	defer sock.Close()
	read := 0
	buf := make([]byte, packetSize)
	for {
		n, err := sock.Read(buf)
		read += n
		if err != nil {
			break
		}
	}
	timed := time.Since(start)

	fmt.Printf("%f packets per second\n", float64(numPackets)/timed.Seconds())
	fmt.Printf("%f bits/sec\n", 8*float64(read)/timed.Seconds())
}
