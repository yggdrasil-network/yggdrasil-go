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

  addr, err := net.ResolveUDPAddr("udp", "[::1]:0")
  if err != nil { panic(err) }
  sock, err := net.ListenUDP("udp", addr)
  if err != nil { panic(err) }
  defer sock.Close()

  go func () {
    raddr := sock.LocalAddr().(*net.UDPAddr)
    send, err := net.DialUDP("udp", nil, raddr)
    //send, err := net.ListenUDP("udp", addr)
    if err != nil { panic(err) }
    defer send.Close()
    msg := make([]byte, 1280)
    for {
      send.Write(msg)
      //send.WriteToUDP(msg, raddr)
    }
  }()

  numPackets := 1000000
  start := time.Now()
  msg := make([]byte, 2000)
  for i := 0 ; i < numPackets ; i++ {
    sock.ReadFromUDP(msg)
  }
  timed := time.Since(start)

  fmt.Printf("%f packets per second\n", float64(numPackets)/timed.Seconds())

}

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile `file`")
var memprofile = flag.String("memprofile", "", "write memory profile to this file")

func main () {
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
      defer func () { pprof.WriteHeapProfile(f) ; f.Close() }()
  }
  basic_test()

}

