package yggdrasil

import (
	"bytes"
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
	destination boxPubKey
}

// Initialise crypto-key routing. This must be done before any other CKR calls.
func (c *cryptokey) init(core *Core) {
	c.core = core
	c.ipv4routes = make([]cryptokey_route, 0)
	c.ipv6routes = make([]cryptokey_route, 0)
	c.ipv4cache = make(map[address]cryptokey_route, 0)
	c.ipv6cache = make(map[address]cryptokey_route, 0)
	c.ipv4sources = make([]net.IPNet, 0)
	c.ipv6sources = make([]net.IPNet, 0)
}

// Enable or disable crypto-key routing.
func (c *cryptokey) setEnabled(enabled bool) {
	c.enabled = enabled
}

// Check if crypto-key routing is enabled.
func (c *cryptokey) isEnabled() bool {
	return c.enabled
}

// Check whether the given address (with the address length specified in bytes)
// matches either the current node's address, the node's routed subnet or the
// list of subnets specified in IPv4Sources/IPv6Sources.
func (c *cryptokey) isValidSource(addr address, addrlen int) bool {
	ip := net.IP(addr[:addrlen])

	if addrlen == net.IPv6len {
		// Does this match our node's address?
		if bytes.Equal(addr[:16], c.core.router.addr[:16]) {
			return true
		}

		// Does this match our node's subnet?
		if bytes.Equal(addr[:8], c.core.router.subnet[:8]) {
			return true
		}
	}

	// Does it match a configured CKR source?
	if c.isEnabled() {
		// Build our references to the routing sources
		var routingsources *[]net.IPNet

		// Check if the prefix is IPv4 or IPv6
		if addrlen == net.IPv6len {
			routingsources = &c.ipv6sources
		} else if addrlen == net.IPv4len {
			routingsources = &c.ipv4sources
		} else {
			return false
		}

		for _, subnet := range *routingsources {
			if subnet.Contains(ip) {
				return true
			}
		}
	}

	// Doesn't match any of the above
	return false
}

// Adds a source subnet, which allows traffic with these source addresses to
// be tunnelled using crypto-key routing.
func (c *cryptokey) addSourceSubnet(cidr string) error {
	// Is the CIDR we've been given valid?
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	// Get the prefix length and size
	_, prefixsize := ipnet.Mask.Size()

	// Build our references to the routing sources
	var routingsources *[]net.IPNet

	// Check if the prefix is IPv4 or IPv6
	if prefixsize == net.IPv6len*8 {
		routingsources = &c.ipv6sources
	} else if prefixsize == net.IPv4len*8 {
		routingsources = &c.ipv4sources
	} else {
		return errors.New("Unexpected prefix size")
	}

	// Check if we already have this CIDR
	for _, subnet := range *routingsources {
		if subnet.String() == ipnet.String() {
			return errors.New("Source subnet already configured")
		}
	}

	// Add the source subnet
	*routingsources = append(*routingsources, *ipnet)
	c.core.log.Println("Added CKR source subnet", cidr)
	return nil
}

// Adds a destination route for the given CIDR to be tunnelled to the node
// with the given BoxPubKey.
func (c *cryptokey) addRoute(cidr string, dest string) error {
	// Is the CIDR we've been given valid?
	ipaddr, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	// Get the prefix length and size
	_, prefixsize := ipnet.Mask.Size()

	// Build our references to the routing table and cache
	var routingtable *[]cryptokey_route
	var routingcache *map[address]cryptokey_route

	// Check if the prefix is IPv4 or IPv6
	if prefixsize == net.IPv6len*8 {
		routingtable = &c.ipv6routes
		routingcache = &c.ipv6cache
	} else if prefixsize == net.IPv4len*8 {
		routingtable = &c.ipv4routes
		routingcache = &c.ipv4cache
	} else {
		return errors.New("Unexpected prefix size")
	}

	// Is the route an Yggdrasil destination?
	var addr address
	var snet subnet
	copy(addr[:], ipaddr)
	copy(snet[:], ipnet.IP)
	if addr.isValid() || snet.isValid() {
		return errors.New("Can't specify Yggdrasil destination as crypto-key route")
	}
	// Do we already have a route for this subnet?
	for _, route := range *routingtable {
		if route.subnet.String() == ipnet.String() {
			return errors.New(fmt.Sprintf("Route already exists for %s", cidr))
		}
	}
	// Decode the public key
	if bpk, err := hex.DecodeString(dest); err != nil {
		return err
	} else if len(bpk) != boxPubKeyLen {
		return errors.New(fmt.Sprintf("Incorrect key length for %s", dest))
	} else {
		// Add the new crypto-key route
		var key boxPubKey
		copy(key[:], bpk)
		*routingtable = append(*routingtable, cryptokey_route{
			subnet:      *ipnet,
			destination: key,
		})

		// Sort so most specific routes are first
		sort.Slice(*routingtable, func(i, j int) bool {
			im, _ := (*routingtable)[i].subnet.Mask.Size()
			jm, _ := (*routingtable)[j].subnet.Mask.Size()
			return im > jm
		})

		// Clear the cache as this route might change future routing
		// Setting an empty slice keeps the memory whereas nil invokes GC
		for k := range *routingcache {
			delete(*routingcache, k)
		}

		c.core.log.Println("Added CKR destination subnet", cidr)
		return nil
	}

	return errors.New("Unspecified error")
}

// Looks up the most specific route for the given address (with the address
// length specified in bytes) from the crypto-key routing table. An error is
// returned if the address is not suitable or no route was found.
func (c *cryptokey) getPublicKeyForAddress(addr address, addrlen int) (boxPubKey, error) {
	// Check if the address is a valid Yggdrasil address - if so it
	// is exempt from all CKR checking
	if addr.isValid() {
		return boxPubKey{}, errors.New("Cannot look up CKR for Yggdrasil addresses")
	}

	// Build our references to the routing table and cache
	var routingtable *[]cryptokey_route
	var routingcache *map[address]cryptokey_route

	// Check if the prefix is IPv4 or IPv6
	if addrlen == net.IPv6len {
		routingtable = &c.ipv6routes
		routingcache = &c.ipv6cache
	} else if addrlen == net.IPv4len {
		routingtable = &c.ipv4routes
		routingcache = &c.ipv4cache
	} else {
		return boxPubKey{}, errors.New("Unexpected prefix size")
	}

	// Check if there's a cache entry for this addr
	if route, ok := (*routingcache)[addr]; ok {
		return route.destination, nil
	}

	// No cache was found - start by converting the address into a net.IP
	ip := make(net.IP, addrlen)
	copy(ip[:addrlen], addr[:])

	// Check if we have a route. At this point c.ipv6routes should be
	// pre-sorted so that the most specific routes are first
	for _, route := range *routingtable {
		// Does this subnet match the given IP?
		if route.subnet.Contains(ip) {
			// Cache the entry for future packets to get a faster lookup
			(*routingcache)[addr] = route

			// Return the boxPubKey
			return route.destination, nil
		}
	}

	// No route was found if we got to this point
	return boxPubKey{}, errors.New(fmt.Sprintf("No route to %s", ip.String()))
}

// Removes a source subnet, which allows traffic with these source addresses to
// be tunnelled using crypto-key routing.
func (c *cryptokey) removeSourceSubnet(cidr string) error {
	// Is the CIDR we've been given valid?
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	// Get the prefix length and size
	_, prefixsize := ipnet.Mask.Size()

	// Build our references to the routing sources
	var routingsources *[]net.IPNet

	// Check if the prefix is IPv4 or IPv6
	if prefixsize == net.IPv6len*8 {
		routingsources = &c.ipv6sources
	} else if prefixsize == net.IPv4len*8 {
		routingsources = &c.ipv4sources
	} else {
		return errors.New("Unexpected prefix size")
	}

	// Check if we already have this CIDR
	for idx, subnet := range *routingsources {
		if subnet.String() == ipnet.String() {
			*routingsources = append((*routingsources)[:idx], (*routingsources)[idx+1:]...)
			c.core.log.Println("Removed CKR source subnet", cidr)
			return nil
		}
	}
	return errors.New("Source subnet not found")
}

// Removes a destination route for the given CIDR to be tunnelled to the node
// with the given BoxPubKey.
func (c *cryptokey) removeRoute(cidr string, dest string) error {
	// Is the CIDR we've been given valid?
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	// Get the prefix length and size
	_, prefixsize := ipnet.Mask.Size()

	// Build our references to the routing table and cache
	var routingtable *[]cryptokey_route
	var routingcache *map[address]cryptokey_route

	// Check if the prefix is IPv4 or IPv6
	if prefixsize == net.IPv6len*8 {
		routingtable = &c.ipv6routes
		routingcache = &c.ipv6cache
	} else if prefixsize == net.IPv4len*8 {
		routingtable = &c.ipv4routes
		routingcache = &c.ipv4cache
	} else {
		return errors.New("Unexpected prefix size")
	}

	// Decode the public key
	bpk, err := hex.DecodeString(dest)
	if err != nil {
		return err
	} else if len(bpk) != boxPubKeyLen {
		return errors.New(fmt.Sprintf("Incorrect key length for %s", dest))
	}
	netStr := ipnet.String()

	for idx, route := range *routingtable {
		if bytes.Equal(route.destination[:], bpk) && route.subnet.String() == netStr {
			*routingtable = append((*routingtable)[:idx], (*routingtable)[idx+1:]...)
			for k := range *routingcache {
				delete(*routingcache, k)
			}
			c.core.log.Println("Removed CKR destination subnet %s via %s", cidr, dest)
			return nil
		}
	}
	return errors.New(fmt.Sprintf("Route does not exists for %s", cidr))
}
