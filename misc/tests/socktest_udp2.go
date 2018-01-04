package main

import "fmt"
import "net"
import "time"
import "flag"
import "os"
import "runtime/pprof"

// TODO look into netmap + libpcap to bypass the kernel as much as possible

func basic_test() {

	// TODO need a way to look up who our link-local neighbors are for each iface!

	saddr, err := net.ResolveUDPAddr("udp", "[::1]:9001")
	if err != nil {
		panic(err)
	}
	raddr, err := net.ResolveUDPAddr("udp", "[::1]:9002")
	if err != nil {
		panic(err)
	}

	send, err := net.DialUDP("udp", saddr, raddr)
	if err != nil {
		panic(err)
	}
	defer send.Close()

	recv, err := net.DialUDP("udp", raddr, saddr)
	if err != nil {
		panic(err)
	}
	defer recv.Close()

	go func() {
		msg := make([]byte, 1280)
		for {
			send.Write(msg)
		}
	}()

	numPackets := 1000000
	start := time.Now()
	msg := make([]byte, 2000)
	for i := 0; i < numPackets; i++ {
		recv.Read(msg)
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
