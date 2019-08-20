package tuntap

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

// This module implements crypto-key routing, similar to Wireguard, where we
// allow traffic for non-Yggdrasil ranges to be routed over Yggdrasil.

type cryptokey struct {
	tun          *TunAdapter
	enabled      atomic.Value // bool
	reconfigure  chan chan error
	ipv4remotes  []cryptokey_route
	ipv6remotes  []cryptokey_route
	ipv4cache    map[address.Address]cryptokey_route
	ipv6cache    map[address.Address]cryptokey_route
	ipv4locals   []net.IPNet
	ipv6locals   []net.IPNet
	mutexremotes sync.RWMutex
	mutexcaches  sync.RWMutex
	mutexlocals  sync.RWMutex
}

type cryptokey_route struct {
	subnet      net.IPNet
	destination crypto.BoxPubKey
}

// Initialise crypto-key routing. This must be done before any other CKR calls.
func (c *cryptokey) init(tun *TunAdapter) {
	c.tun = tun
	c.reconfigure = make(chan chan error, 1)
	go func() {
		for {
			e := <-c.reconfigure
			e <- nil
		}
	}()

	c.tun.log.Debugln("Configuring CKR...")
	if err := c.configure(); err != nil {
		c.tun.log.Errorln("CKR configuration failed:", err)
	} else {
		c.tun.log.Debugln("CKR configured")
	}
}

// Configure the CKR routes - this must only ever be called from the router
// goroutine, e.g. through router.doAdmin
func (c *cryptokey) configure() error {
	current := c.tun.config.GetCurrent()

	// Set enabled/disabled state
	c.setEnabled(current.TunnelRouting.Enable)

	// Clear out existing routes
	c.mutexremotes.Lock()
	c.ipv6remotes = make([]cryptokey_route, 0)
	c.ipv4remotes = make([]cryptokey_route, 0)
	c.mutexremotes.Unlock()

	// Add IPv6 routes
	for ipv6, pubkey := range current.TunnelRouting.IPv6RemoteSubnets {
		if err := c.addRemoteSubnet(ipv6, pubkey); err != nil {
			return err
		}
	}

	// Add IPv4 routes
	for ipv4, pubkey := range current.TunnelRouting.IPv4RemoteSubnets {
		if err := c.addRemoteSubnet(ipv4, pubkey); err != nil {
			return err
		}
	}

	// Clear out existing sources
	c.mutexlocals.Lock()
	c.ipv6locals = make([]net.IPNet, 0)
	c.ipv4locals = make([]net.IPNet, 0)
	c.mutexlocals.Unlock()

	// Add IPv6 sources
	c.ipv6locals = make([]net.IPNet, 0)
	for _, source := range current.TunnelRouting.IPv6LocalSubnets {
		if err := c.addLocalSubnet(source); err != nil {
			return err
		}
	}

	// Add IPv4 sources
	c.ipv4locals = make([]net.IPNet, 0)
	for _, source := range current.TunnelRouting.IPv4LocalSubnets {
		if err := c.addLocalSubnet(source); err != nil {
			return err
		}
	}

	// Wipe the caches
	c.mutexcaches.Lock()
	c.ipv4cache = make(map[address.Address]cryptokey_route, 0)
	c.ipv6cache = make(map[address.Address]cryptokey_route, 0)
	c.mutexcaches.Unlock()

	return nil
}

// Enable or disable crypto-key routing.
func (c *cryptokey) setEnabled(enabled bool) {
	c.enabled.Store(enabled)
}

// Check if crypto-key routing is enabled.
func (c *cryptokey) isEnabled() bool {
	enabled, ok := c.enabled.Load().(bool)
	return ok && enabled
}

// Check whether the given address (with the address length specified in bytes)
// matches either the current node's address, the node's routed subnet or the
// list of subnets specified in ipv4locals/ipv6locals.
func (c *cryptokey) isValidLocalAddress(addr address.Address, addrlen int) bool {
	c.mutexlocals.RLock()
	defer c.mutexlocals.RUnlock()

	ip := net.IP(addr[:addrlen])

	if addrlen == net.IPv6len {
		// Does this match our node's address?
		if bytes.Equal(addr[:16], c.tun.addr[:16]) {
			return true
		}

		// Does this match our node's subnet?
		if bytes.Equal(addr[:8], c.tun.subnet[:8]) {
			return true
		}
	}

	// Does it match a configured CKR source?
	if c.isEnabled() {
		// Build our references to the routing sources
		var routingsources *[]net.IPNet

		// Check if the prefix is IPv4 or IPv6
		if addrlen == net.IPv6len {
			routingsources = &c.ipv6locals
		} else if addrlen == net.IPv4len {
			routingsources = &c.ipv4locals
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
func (c *cryptokey) addLocalSubnet(cidr string) error {
	c.mutexlocals.Lock()
	defer c.mutexlocals.Unlock()

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
		routingsources = &c.ipv6locals
	} else if prefixsize == net.IPv4len*8 {
		routingsources = &c.ipv4locals
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
	c.tun.log.Infoln("Added CKR source subnet", cidr)
	return nil
}

// Adds a destination route for the given CIDR to be tunnelled to the node
// with the given BoxPubKey.
func (c *cryptokey) addRemoteSubnet(cidr string, dest string) error {
	c.mutexremotes.Lock()
	c.mutexcaches.Lock()
	defer c.mutexremotes.Unlock()
	defer c.mutexcaches.Unlock()

	// Is the CIDR we've been given valid?
	ipaddr, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	// Get the prefix length and size
	_, prefixsize := ipnet.Mask.Size()

	// Build our references to the routing table and cache
	var routingtable *[]cryptokey_route
	var routingcache *map[address.Address]cryptokey_route

	// Check if the prefix is IPv4 or IPv6
	if prefixsize == net.IPv6len*8 {
		routingtable = &c.ipv6remotes
		routingcache = &c.ipv6cache
	} else if prefixsize == net.IPv4len*8 {
		routingtable = &c.ipv4remotes
		routingcache = &c.ipv4cache
	} else {
		return errors.New("Unexpected prefix size")
	}

	// Is the route an Yggdrasil destination?
	var addr address.Address
	var snet address.Subnet
	copy(addr[:], ipaddr)
	copy(snet[:], ipnet.IP)
	if addr.IsValid() || snet.IsValid() {
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
	} else if len(bpk) != crypto.BoxPubKeyLen {
		return errors.New(fmt.Sprintf("Incorrect key length for %s", dest))
	} else {
		// Add the new crypto-key route
		var key crypto.BoxPubKey
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

		c.tun.log.Infoln("Added CKR destination subnet", cidr)
		return nil
	}
}

// Looks up the most specific route for the given address (with the address
// length specified in bytes) from the crypto-key routing table. An error is
// returned if the address is not suitable or no route was found.
func (c *cryptokey) getPublicKeyForAddress(addr address.Address, addrlen int) (crypto.BoxPubKey, error) {
	c.mutexcaches.RLock()

	// Check if the address is a valid Yggdrasil address - if so it
	// is exempt from all CKR checking
	if addr.IsValid() {
		return crypto.BoxPubKey{}, errors.New("Cannot look up CKR for Yggdrasil addresses")
	}

	// Build our references to the routing table and cache
	var routingtable *[]cryptokey_route
	var routingcache *map[address.Address]cryptokey_route

	// Check if the prefix is IPv4 or IPv6
	if addrlen == net.IPv6len {
		routingcache = &c.ipv6cache
	} else if addrlen == net.IPv4len {
		routingcache = &c.ipv4cache
	} else {
		return crypto.BoxPubKey{}, errors.New("Unexpected prefix size")
	}

	// Check if there's a cache entry for this addr
	if route, ok := (*routingcache)[addr]; ok {
		c.mutexcaches.RUnlock()
		return route.destination, nil
	}

	c.mutexcaches.RUnlock()

	c.mutexremotes.RLock()
	defer c.mutexremotes.RUnlock()

	// Check if the prefix is IPv4 or IPv6
	if addrlen == net.IPv6len {
		routingtable = &c.ipv6remotes
	} else if addrlen == net.IPv4len {
		routingtable = &c.ipv4remotes
	} else {
		return crypto.BoxPubKey{}, errors.New("Unexpected prefix size")
	}

	// No cache was found - start by converting the address into a net.IP
	ip := make(net.IP, addrlen)
	copy(ip[:addrlen], addr[:])

	// Check if we have a route. At this point c.ipv6remotes should be
	// pre-sorted so that the most specific routes are first
	for _, route := range *routingtable {
		// Does this subnet match the given IP?
		if route.subnet.Contains(ip) {
			c.mutexcaches.Lock()
			defer c.mutexcaches.Unlock()

			// Check if the routing cache is above a certain size, if it is evict
			// a random entry so we can make room for this one. We take advantage
			// of the fact that the iteration order is random here
			for k := range *routingcache {
				if len(*routingcache) < 1024 {
					break
				}
				delete(*routingcache, k)
			}

			// Cache the entry for future packets to get a faster lookup
			(*routingcache)[addr] = route

			// Return the boxPubKey
			return route.destination, nil
		}
	}

	// No route was found if we got to this point
	return crypto.BoxPubKey{}, fmt.Errorf("no route to %s", ip.String())
}

// Removes a source subnet, which allows traffic with these source addresses to
// be tunnelled using crypto-key routing.
func (c *cryptokey) removeLocalSubnet(cidr string) error {
	c.mutexlocals.Lock()
	defer c.mutexlocals.Unlock()

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
		routingsources = &c.ipv6locals
	} else if prefixsize == net.IPv4len*8 {
		routingsources = &c.ipv4locals
	} else {
		return errors.New("Unexpected prefix size")
	}

	// Check if we already have this CIDR
	for idx, subnet := range *routingsources {
		if subnet.String() == ipnet.String() {
			*routingsources = append((*routingsources)[:idx], (*routingsources)[idx+1:]...)
			c.tun.log.Infoln("Removed CKR source subnet", cidr)
			return nil
		}
	}
	return errors.New("Source subnet not found")
}

// Removes a destination route for the given CIDR to be tunnelled to the node
// with the given BoxPubKey.
func (c *cryptokey) removeRemoteSubnet(cidr string, dest string) error {
	c.mutexremotes.Lock()
	c.mutexcaches.Lock()
	defer c.mutexremotes.Unlock()
	defer c.mutexcaches.Unlock()

	// Is the CIDR we've been given valid?
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	// Get the prefix length and size
	_, prefixsize := ipnet.Mask.Size()

	// Build our references to the routing table and cache
	var routingtable *[]cryptokey_route
	var routingcache *map[address.Address]cryptokey_route

	// Check if the prefix is IPv4 or IPv6
	if prefixsize == net.IPv6len*8 {
		routingtable = &c.ipv6remotes
		routingcache = &c.ipv6cache
	} else if prefixsize == net.IPv4len*8 {
		routingtable = &c.ipv4remotes
		routingcache = &c.ipv4cache
	} else {
		return errors.New("Unexpected prefix size")
	}

	// Decode the public key
	bpk, err := hex.DecodeString(dest)
	if err != nil {
		return err
	} else if len(bpk) != crypto.BoxPubKeyLen {
		return errors.New(fmt.Sprintf("Incorrect key length for %s", dest))
	}
	netStr := ipnet.String()

	for idx, route := range *routingtable {
		if bytes.Equal(route.destination[:], bpk) && route.subnet.String() == netStr {
			*routingtable = append((*routingtable)[:idx], (*routingtable)[idx+1:]...)
			for k := range *routingcache {
				delete(*routingcache, k)
			}
			c.tun.log.Infof("Removed CKR destination subnet %s via %s\n", cidr, dest)
			return nil
		}
	}
	return errors.New(fmt.Sprintf("Route does not exists for %s", cidr))
}
