// +build linux

package multicast

import (
	"fmt"
	"net"
	"regexp"
	"syscall"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func (m *Multicast) _multicastStarted() {
	linkChanges := make(chan netlink.LinkUpdate)
	addrChanges := make(chan netlink.AddrUpdate)

	linkClose := make(chan struct{})
	addrClose := make(chan struct{})

	linkSubscribeOptions := netlink.LinkSubscribeOptions{
		ListExisting: true,
	}

	if err := netlink.LinkSubscribeWithOptions(linkChanges, linkClose, linkSubscribeOptions); err != nil {
		panic(err)
	}

	if err := netlink.AddrSubscribe(addrChanges, addrClose); err != nil {
		panic(err)
	}

	fmt.Println("Listening for netlink changes")

	go func() {
		defer fmt.Println("No longer listening for netlink changes")
		for {
			current := m.config.GetCurrent()
			exprs := current.MulticastInterfaces

			select {
			case change := <-linkChanges:
				attrs := change.Attrs()
				add := true
				add = add && attrs.Flags&net.FlagUp == 1
				//add = add && attrs.Flags&net.FlagMulticast == 1
				//add = add && attrs.Flags&net.FlagPointToPoint == 0

				match := false
				for _, expr := range exprs {
					e, err := regexp.Compile(expr)
					if err != nil {
						panic(err)
					}
					if e.MatchString(attrs.Name) {
						match = true
						break
					}
				}
				add = add && match

				if add {
					m.Act(nil, func() {
						fmt.Println("Link added:", attrs.Name)
						if iface, err := net.InterfaceByName(attrs.Name); err == nil {
							if addrs, err := iface.Addrs(); err == nil {
								m._interfaces[attrs.Name] = interfaceInfo{
									iface: *iface,
									addrs: addrs,
								}
							}
						}
					})
				} else {
					m.Act(nil, func() {
						fmt.Println("Link removed:", attrs.Name)
						delete(m._interfaces, attrs.Name)
					})
				}

			case change := <-addrChanges:
				m.Act(nil, func() {
					fmt.Println("Addr changed:", change)
				})

			case <-linkClose:
				return

			case <-addrClose:
				return
			}
		}
	}()
}

func (m *Multicast) multicastReuse(network string, address string, c syscall.RawConn) error {
	var control error
	var reuseport error

	control = c.Control(func(fd uintptr) {
		reuseport = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	})

	switch {
	case reuseport != nil:
		return reuseport
	default:
		return control
	}
}
