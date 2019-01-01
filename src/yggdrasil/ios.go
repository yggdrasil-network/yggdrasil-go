// +build mobile

package yggdrasil

import (
	"log"
	"os"
	"regexp"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// This file is meant to "plug the gap" for Gomobile support, as Gomobile
// will not create headers for Swift/Obj-C if they have complex (read: non-
// native) types. Therefore for iOS we will expose some nice simple functions
// to do what we need to do.

func (c *Core) StartAutoconfigure() error {
	logger := log.New(os.Stdout, "", 0)
	//logger.Println("Created logger")
	//c := Core{}
	//logger.Println("Created Core")
	nc := config.GenerateConfig(true)
	//logger.Println("Generated config")
	nc.IfName = "none"
	nc.AdminListen = "tcp://[::]:9001"
	nc.Peers = []string{}
	//logger.Println("Set some config options")
	ifceExpr, err := regexp.Compile(".*")
	if err == nil {
		c.ifceExpr = append(c.ifceExpr, ifceExpr)
	}
	//logger.Println("Added multicast interface")
	if err := c.Start(nc, logger); err != nil {
		return err
	}
	//logger.Println("Started")
	address := c.GetAddress()
	subnet := c.GetSubnet()
	logger.Printf("Your IPv6 address is %s", address.String())
	logger.Printf("Your IPv6 subnet is %s", subnet.String())
	return nil
}

func (c *Core) GetAddressString() string {
	return c.GetAddress().String()
}

func (c *Core) GetSubetString() string {
	return c.GetSubnet().String()
}
