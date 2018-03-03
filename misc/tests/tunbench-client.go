package main

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"time"

	"github.com/neilalexander/water"
)

const mtu = 65535

func setup_dev() *water.Interface {
	ifce, err := water.New(water.Config{
		DeviceType: water.TUN,
	})
	if err != nil {
		panic(err)
	}
	return ifce
}

func setup_dev1() *water.Interface {
	ifce := setup_dev()
	cmd := exec.Command("ip", "-f", "inet6",
		"addr", "add", "fc00::2/8",
		"dev", ifce.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		panic("Failed to assign address")
	}
	cmd = exec.Command("ip", "link", "set",
		"dev", ifce.Name(),
		"mtu", fmt.Sprintf("%d", mtu),
		"up")
	out, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		panic("Failed to bring up interface")
	}
	return ifce
}

func connect(ifce *water.Interface) {
	conn, err := net.DialTimeout("tcp", "192.168.2.2:9001", time.Second)
	if err != nil {
		panic(err)
	}
	sock := conn.(*net.TCPConn)
	// TODO go a worker to move packets to/from the tun
}

func bench() {
}

func main() {
	ifce := setup_dev1()
	connect(ifce)
	bench()
	fmt.Println("Done?")
	return
	ifce, err := water.New(water.Config{
		DeviceType: water.TUN,
	})
	if err != nil {
		panic(err)
	}

	log.Printf("Interface Name: %s\n", ifce.Name())

	packet := make([]byte, 2000)
	for {
		n, err := ifce.Read(packet)
		if err != nil {
			panic(err)
			log.Fatal(err)
		}
		log.Printf("Packet Received: % x\n", packet[:n])
	}
}
