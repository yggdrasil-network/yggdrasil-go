package yggdrasil

import (
	"encoding/hex"
	"errors"
	"net"
	"sort"
)

// This module implements crypto-key routing, similar to Wireguard, where we
// allow traffic for non-Yggdrasil ranges to be routed over Yggdrasil.

type cryptokey struct {
	core       *Core
	enabled    bool
	ipv4routes []cryptokey_route
	ipv6routes []cryptokey_route
}

type cryptokey_route struct {
	subnet      net.IPNet
	destination []byte
}

func (c *cryptokey) init(core *Core) {
	c.core = core
	c.ipv4routes = make([]cryptokey_route, 0)
	c.ipv6routes = make([]cryptokey_route, 0)
}

func (c *cryptokey) isEnabled() bool {
	return c.enabled
}

func (c *cryptokey) addRoute(cidr string, dest string) error {
	// Is the CIDR we've been given valid?
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	// Get the prefix length and size
	prefixlen, prefixsize := ipnet.Mask.Size()

	// Check if the prefix is IPv4 or IPv6
	if prefixsize == net.IPv6len*8 {
		// IPv6
		for _, route := range c.ipv6routes {
			// Do we already have a route for this subnet?
			routeprefixlen, _ := route.subnet.Mask.Size()
			if route.subnet.IP.Equal(ipnet.IP) && routeprefixlen == prefixlen {
				return errors.New("IPv6 route already exists")
			}
		}
		// Decode the public key
		if boxPubKey, err := hex.DecodeString(dest); err != nil {
			return err
		} else {
			// Add the new crypto-key route
			c.ipv6routes = append(c.ipv6routes, cryptokey_route{
				subnet:      *ipnet,
				destination: boxPubKey,
			})
			// Sort so most specific routes are first
			sort.Slice(c.ipv6routes, func(i, j int) bool {
				im, _ := c.ipv6routes[i].subnet.Mask.Size()
				jm, _ := c.ipv6routes[j].subnet.Mask.Size()
				return im > jm
			})
			return nil
		}
	} else if prefixsize == net.IPv4len*8 {
		// IPv4
		return errors.New("IPv4 not supported at this time")
	}
	return errors.New("Unspecified error")
}

func (c *cryptokey) getPublicKeyForAddress(addr string) (boxPubKey, error) {
	ipaddr := net.ParseIP(addr)

	if ipaddr.To4() == nil {
		// IPv6
		for _, route := range c.ipv6routes {
			if route.subnet.Contains(ipaddr) {
				var box boxPubKey
				copy(box[:boxPubKeyLen], route.destination)
				return box, nil
			}
		}
	} else {
		// IPv4
		return boxPubKey{}, errors.New("IPv4 not supported at this time")
		/*
			    for _, route := range c.ipv4routes {
						if route.subnet.Contains(ipaddr) {
							return route.destination, nil
						}
					}
		*/
	}

	return boxPubKey{}, errors.New("No route")
}
