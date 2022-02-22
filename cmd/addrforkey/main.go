/*

This file converts a public to to an IPv6 address. Example:
$ go run main.go <pubkey>.
IP: <ipv6ip>

*/
package main

import (
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

func main() {
        pub, err := hex.DecodeString(os.Args[1])
	if err != nil {
                fmt.Println("Failed to decode provided public key")
                os.Exit(1)
	}
        addr := address.AddrForKey(pub)
        fmt.Println("IP:", net.IP(addr[:]).String())
}
