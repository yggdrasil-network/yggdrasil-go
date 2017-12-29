package main

import "fmt"
import "net"
import "time"
import "flag"
import "os"
import "runtime/pprof"

import "golang.org/x/net/ipv6"

// TODO look into netmap + libpcap to bypass the kernel as much as possible

func basic_test() {

  // TODO need a way to look up who our link-local neighbors are for each iface!

  udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
  if err != nil { panic(err) }
  sock, err := net.ListenUDP("udp", udpAddr)
  if err != nil { panic(err) }
  defer sock.Close()

  writer := func () {
    raddr := sock.LocalAddr().(*net.UDPAddr)
    send, err := net.ListenUDP("udp", nil)
    if err != nil { panic(err) }
    defer send.Close()
    conn := ipv6.NewPacketConn(send)
    defer conn.Close()
    var msgs []ipv6.Message
    for idx := 0 ; idx < 1024 ; idx++ {
      msg := ipv6.Message{Addr: raddr, Buffers: [][]byte{make([]byte, 1280)}}
      msgs = append(msgs, msg)
    }
    for {
      /*
      var msgs []ipv6.Message
      for idx := 0 ; idx < 1024 ; idx++ {
        msg := ipv6.Message{Addr: raddr, Buffers: [][]byte{make([]byte, 1280)}}
        msgs = append(msgs, msg)
      }
      */
      conn.WriteBatch(msgs, 0)
    }

  }
  go writer()
  //go writer()
  //go writer()
  //go writer()

  numPackets := 65536
  size := 0
  count := 0
  start := time.Now()
  /*
  conn := ipv6.NewPacketConn(sock)
  defer conn.Close()
  for ; count < numPackets ; count++ {
    msgs := make([]ipv6.Message, 1024)
    for _, msg := range msgs {
      msg.Buffers = append(msg.Buffers, make([]byte, 2048))
    }
    n, err := conn.ReadBatch(msgs, 0)
    if err != nil { panic(err) }
    fmt.Println("DEBUG: n", n)
    for _, msg := range msgs[:n] {
      fmt.Println("DEBUG: msg", msg)
      size += msg.N
      //for _, bs := range msg.Buffers {
      //  size += len(bs)
      //}
      count++
    }
  }
  //*/
  //*
  for ; count < numPackets ; count++ {
    msg := make([]byte, 2048)
    n, _, err := sock.ReadFromUDP(msg)
    if err != nil { panic(err) }
    size += n
  }
  //*/
  timed := time.Since(start)

  fmt.Printf("%f packets per second\n", float64(count)/timed.Seconds())
  fmt.Printf("%f bits/second\n", float64(8*size)/timed.Seconds())
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

