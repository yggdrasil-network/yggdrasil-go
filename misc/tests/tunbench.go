package main

import (
	"fmt"
	"log"
	"net"
	"os/exec"

	"github.com/neilalexander/water"
)

const mtu = 65535
const netnsName = "tunbenchns"

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
		"addr", "add", "fc00::1/8",
		"dev", ifce.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		fmt.Println(string(err))
		panic("Failed to assign address")
	}
	cmd = exec.Command("ip", "link", "set",
		"dev", tun.name,
		"mtu", fmt.Sprintf("%d", mtu),
		"up")
	out, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		panic("Failed to bring up interface")
	}
	return ifce
}

func addNS(name string) {
	cmd := exec.COmmand("ip", "netns", "add", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		panic("Failed to setup netns")
	}
}

func delNS(name string) {
	cmd := exec.COmmand("ip", "netns", "delete", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		panic("Failed to setup netns")
	}
}

func doInNetNS(comm ...string) *exec.Cmd {
	return exec.Command("ip", "netns", "exec", netnsName, comm...)
}

func setup_dev2() *water.Interface {
	ifce := setup_dev()
	addNS(netnsName)
	cmd := exec.Command("ip", "link", "set", ifce.Name(), "netns", netnsName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		panic("Failed to move tun to netns")
	}
	cmd = exec.Command(
		"ip", "-f", "inet6",
		"addr", "add", "fc00::2/8",
		"dev", ifce.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		panic("Failed to assign address")
	}
	cmd = exec.Command(
		"ip", "link", "set",
		"dev", tun.name,
		"mtu", fmt.Sprintf("%d", mtu),
		"up")
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		fmt.Println(string(err))
		panic("Failed to bring up interface")
	}
	return ifce
}

func connect() {

}

func bench() {
}

func main() {
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
