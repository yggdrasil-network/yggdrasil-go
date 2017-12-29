package main

import "fmt"
import "net"
import "time"

// TODO look into netmap + libpcap to bypass the kernel as much as possible

func basic_test() {

  // TODO need a way to look up who our link-local neighbors are for each iface!
  //addr, err := net.ResolveUDPAddr("udp", "[ff02::1%veth0]:9001")
  addr, err := net.ResolveUDPAddr("udp", "[ff02::1]:9001")
  if err != nil { panic(err) }
  sock, err := net.ListenMulticastUDP("udp", nil, addr)
  if err != nil { panic(err) }
  defer sock.Close()

  go func () {
    saddr, err := net.ResolveUDPAddr("udp", "[::]:0")
    if err != nil { panic(err) }
    send, err := net.ListenUDP("udp", saddr)
    if err != nil { panic(err) }
    defer send.Close()
    msg := make([]byte, 1280)
    for {
      //fmt.Println("Sending...")
      send.WriteTo(msg, addr)
    }
  }()

  numPackets := 1000
  start := time.Now()
  msg := make([]byte, 2000)
  for i := 0 ; i < numPackets ; i++ {
    //fmt.Println("Reading:", i)
    sock.ReadFromUDP(msg)
  }
  timed := time.Since(start)

  fmt.Printf("%f packets per second\n", float64(numPackets)/timed.Seconds())

}

func main () {

  basic_test()

}
