package main

import "fmt"
import "net"
import "time"

// TODO look into netmap + libpcap to bypass the kernel as much as possible

func basic_test() {

	// TODO need a way to look up who our link-local neighbors are for each iface!

	var ip *net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}
	var zone string
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			panic(err)
		}
		for _, addr := range addrs {
			addrIP, _, _ := net.ParseCIDR(addr.String())
			if addrIP.To4() != nil {
				continue
			} // IPv6 only
			if !addrIP.IsLinkLocalUnicast() {
				continue
			}
			zone = iface.Name
			ip = &addrIP
		}
		addrs, err = iface.MulticastAddrs()
		if err != nil {
			panic(err)
		}
		for _, addr := range addrs {
			fmt.Println(addr.String())
		}
	}
	if ip == nil {
		panic("No link-local IPv6 found")
	}
	fmt.Println("Using address:", *ip)

	addr := net.UDPAddr{IP: *ip, Port: 9001, Zone: zone}

	saddr := net.UDPAddr{IP: *ip, Port: 9002, Zone: zone}
	send, err := net.ListenUDP("udp", &saddr)
	defer send.Close()
	if err != nil {
		panic(err)
	}
	sock, err := net.ListenUDP("udp", &addr)
	defer sock.Close()
	if err != nil {
		panic(err)
	}

	const buffSize = 1048576 * 100

	send.SetWriteBuffer(buffSize)
	sock.SetReadBuffer(buffSize)
	sock.SetWriteBuffer(buffSize)

	go func() {
		msg := make([]byte, 1280)
		for {
			send.WriteTo(msg, &addr)
		}
	}()

	numPackets := 100000
	start := time.Now()
	msg := make([]byte, 2000)
	for i := 0; i < numPackets; i++ {
		_, addr, _ := sock.ReadFrom(msg)
		sock.WriteTo(msg, addr)
	}
	timed := time.Since(start)

	fmt.Printf("%f packets per second\n", float64(numPackets)/timed.Seconds())

}

func main() {

	basic_test()

}
