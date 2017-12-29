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

  sock, err := net.ListenUDP("udp", nil)
  if err != nil { panic(err) }
  defer sock.Close()

  ch := make(chan []byte, 1)

  writer := func () {
    raddr := sock.LocalAddr().(*net.UDPAddr)
    //send, err := net.ListenUDP("udp", nil)
    //if err != nil { panic(err) }
    //defer send.Close()
    for {
      select {
        case <-ch:
        default:
      }
      msg := make([]byte, 1280)
      sock.WriteToUDP(msg, raddr)
      //send.WriteToUDP(msg, raddr)
    }
  }
  go writer()
  //go writer()
  //go writer()
  //go writer()

  numPackets := 65536
  size := 0
  start := time.Now()
  success := 0
  for i := 0 ; i < numPackets ; i++ {
    msg := make([]byte, 2048)
    n, _, err := sock.ReadFromUDP(msg)
    if err != nil { panic(err) }
    size += n
    select {
      case ch <- msg: success += 1
      default:
    }
  }
  timed := time.Since(start)

  fmt.Printf("%f packets per second\n", float64(numPackets)/timed.Seconds())
  fmt.Printf("%f bits per second\n", 8*float64(size)/timed.Seconds())
  fmt.Println("Success:", success, "/", numPackets)
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

