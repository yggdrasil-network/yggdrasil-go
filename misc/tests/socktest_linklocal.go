package main

import "flag"
import "fmt"
import "net"
import "os"
import "runtime/pprof"
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
			fmt.Println(iface.Name, addrIP)
			zone = iface.Name
			ip = &addrIP
		}
		if ip != nil {
			break
		}
		/*
		   addrs, err = iface.MulticastAddrs()
		   if err != nil { panic(err) }
		   for _, addr := range addrs {
		     fmt.Println(addr.String())
		   }
		*/
	}
	if ip == nil {
		panic("No link-local IPv6 found")
	}
	fmt.Println("Using address:", *ip)
	addr := net.UDPAddr{IP: *ip, Port: 9001, Zone: zone}

	laddr, err := net.ResolveUDPAddr("udp", "[::]:9001")
	if err != nil {
		panic(err)
	}
	sock, err := net.ListenUDP("udp", laddr)
	if err != nil {
		panic(err)
	}
	defer sock.Close()

	go func() {
		send, err := net.DialUDP("udp", nil, &addr)
		//send, err := net.ListenUDP("udp", nil)
		if err != nil {
			panic(err)
		}
		defer send.Close()
		msg := make([]byte, 1280)
		for {
			send.Write(msg)
			//send.WriteToUDP(msg, &addr)
		}
	}()

	numPackets := 1000000
	start := time.Now()
	msg := make([]byte, 2000)
	for i := 0; i < numPackets; i++ {
		sock.ReadFromUDP(msg)
	}
	timed := time.Since(start)

	fmt.Printf("%f packets per second\n", float64(numPackets)/timed.Seconds())

}

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile `file`")
var memprofile = flag.String("memprofile", "", "write memory profile to this file")

func main() {
	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			panic(fmt.Sprintf("could not create CPU profile: ", err))
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			panic(fmt.Sprintf("could not start CPU profile: ", err))
		}
		defer pprof.StopCPUProfile()
	}
	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			panic(fmt.Sprintf("could not create memory profile: ", err))
		}
		defer func() { pprof.WriteHeapProfile(f); f.Close() }()
	}
	basic_test()

}
