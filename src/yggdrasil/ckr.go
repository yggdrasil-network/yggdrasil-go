package yggdrasil

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
)

// This module implements crypto-key routing, similar to Wireguard, where we
// allow traffic for non-Yggdrasil ranges to be routed over Yggdrasil.

type cryptokey struct {
	core        *Core
	enabled     bool
	ipv4routes  []cryptokey_route
	ipv6routes  []cryptokey_route
	ipv4cache   map[address]cryptokey_route
	ipv6cache   map[address]cryptokey_route
	ipv4sources []net.IPNet
	ipv6sources []net.IPNet
}

type cryptokey_route struct {
	subnet      net.IPNet
	destination []byte
}

func (c *cryptokey) init(core *Core) {
	c.core = core
	c.ipv4routes = make([]cryptokey_route, 0)
	c.ipv6routes = make([]cryptokey_route, 0)
	c.ipv4cache = make(map[address]cryptokey_route, 0)
	c.ipv6cache = make(map[address]cryptokey_route, 0)
	c.ipv4sources = make([]net.IPNet, 0)
	c.ipv6sources = make([]net.IPNet, 0)
}

func (c *cryptokey) isEnabled() bool {
	return c.enabled
}

func (c *cryptokey) isValidSource(addr address) bool {
	ip := net.IP(addr[:])

	// Does this match our node's address?
	if addr == c.core.router.addr {
		return true
	}

	// Does this match our node's subnet?
	var subnet net.IPNet
	copy(subnet.IP, c.core.router.subnet[:])
	copy(subnet.Mask, net.CIDRMask(64, 128))
	if subnet.Contains(ip) {
		return true
	}

	// Does it match a configured CKR source?
	for _, subnet := range c.ipv6sources {
		if subnet.Contains(ip) {
			return true
		}
	}

	// Doesn't match any of the above
	return false
}

func (c *cryptokey) addSourceSubnet(cidr string) error {
	// Is the CIDR we've been given valid?
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	// Check if we already have this CIDR
	for _, subnet := range c.ipv6sources {
		if subnet.String() == ipnet.String() {
			return errors.New("Source subnet already configured")
		}
	}

	// Add the source subnet
	c.ipv6sources = append(c.ipv6sources, *ipnet)
	return nil
}

func (c *cryptokey) addRoute(cidr string, dest string) error {
	// Is the CIDR we've been given valid?
	ipaddr, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	// Get the prefix length and size
	_, prefixsize := ipnet.Mask.Size()

	// Check if the prefix is IPv4 or IPv6
	if prefixsize == net.IPv6len*8 {
		// Is the route an Yggdrasil destination?
		var addr address
		var snet subnet
		copy(addr[:], ipaddr)
		copy(snet[:], ipnet.IP)
		if addr.isValid() || snet.isValid() {
			return errors.New("Can't specify Yggdrasil destination as crypto-key route")
		}
		// Do we already have a route for this subnet?
		for _, route := range c.ipv6routes {
			if route.subnet.String() == ipnet.String() {
				return errors.New(fmt.Sprintf("Route already exists for %s", cidr))
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

			// Clear the cache as this route might change future routing
			// Setting an empty slice keeps the memory whereas nil invokes GC
			for k := range c.ipv6cache {
				delete(c.ipv6cache, k)
			}

			return nil
		}
	} else if prefixsize == net.IPv4len*8 {
		// IPv4
		return errors.New("IPv4 not supported at this time")
	}
	return errors.New("Unspecified error")
}

func (c *cryptokey) getPublicKeyForAddress(addr address) (boxPubKey, error) {
	// Check if the address is a valid Yggdrasil address - if so it
	// is exempt from all CKR checking
	if addr.isValid() {
		return boxPubKey{}, errors.New("Cannot look up CKR for Yggdrasil addresses")
	}

	// Check if there's a cache entry for this addr
	if route, ok := c.ipv6cache[addr]; ok {
		var box boxPubKey
		copy(box[:boxPubKeyLen], route.destination)
		return box, nil
	}

	// No cache was found - start by converting the address into a net.IP
	ip := make(net.IP, 16)
	copy(ip[:16], addr[:])

	// Check whether it's an IPv4 or an IPv6 address
	if ip.To4() == nil {
		// Check if we have a route. At this point c.ipv6routes should be
		// pre-sorted so that the most specific routes are first
		for _, route := range c.ipv6routes {
			// Does this subnet match the given IP?
			if route.subnet.Contains(ip) {
				// Cache the entry for future packets to get a faster lookup
				c.ipv6cache[addr] = route

				// Return the boxPubKey
				var box boxPubKey
				copy(box[:boxPubKeyLen], route.destination)
				return box, nil
			}
		}
	} else {
		// IPv4 isn't supported yet
		return boxPubKey{}, errors.New("IPv4 not supported at this time")
	}

	// No route was found if we got to this point
	return boxPubKey{}, errors.New(fmt.Sprintf("No route to %s", ip.String()))
}
