package main

import "fmt"
import "net"
import "time"
import "flag"
import "os"
import "runtime/pprof"

// TODO look into netmap + libpcap to bypass the kernel as much as possible?

const buffSize = 32

func basic_test() {

  // TODO need a way to look up who our link-local neighbors are for each iface!

  addr, err := net.ResolveTCPAddr("tcp", "[::1]:9001")
  if err != nil { panic(err) }
  listener, err := net.ListenTCP("tcp", addr)
  if err != nil { panic(err) }
  defer listener.Close()

  go func () {
    send, err := net.DialTCP("tcp", nil, addr)
    if err != nil { panic(err) }
    defer send.Close()
    msg := make([]byte, 1280)
    bss := make(net.Buffers, 0, 1024)
    count := 0
    for {
      time.Sleep(100*time.Millisecond)
      for len(bss) < count {
        bss = append(bss, msg)
      }
      bss.WriteTo(send)
      count++
      //send.Write(msg)
    }
  }()

  numPackets := 1000000
  start := time.Now()
  //msg := make([]byte, 1280)
  sock, err := listener.AcceptTCP()
  if err != nil { panic(err) }
  defer sock.Close()
  for {
    msg := make([]byte, 1280*buffSize)
    n, err := sock.Read(msg)
    if err != nil { panic(err) }
    msg = msg[:n]
    fmt.Println("Read:", n)
    for len(msg) > 1280 {
      // handle message
      msg = msg[1280:]
    }
    // handle remaining fragment of message
    //fmt.Println(n)
  }
  timed := time.Since(start)

  fmt.Printf("%f packets per second\n", float64(numPackets)/timed.Seconds())

  _ = func (in (chan<- int)) {
    close(in)
  }

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

