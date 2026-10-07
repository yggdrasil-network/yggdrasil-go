//go:build !plan9

package multicast

// Non-Plan-9 platforms: the multicast socket is golang.org/x/net/ipv6's
// PacketConn over a UDP6 listener. The Multicast struct and _start (socket
// creation) live here; the announce/listen/stop logic in multicast.go is
// shared across platforms and calls the sock methods that *ipv6.PacketConn
// provides.

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/Arceliar/phony"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"golang.org/x/net/ipv6"
)

// Multicast represents the multicast advertisement and discovery mechanism used
// by Yggdrasil to find peers on the same subnet. When a beacon is received on a
// configured multicast interface, Yggdrasil will attempt to peer with that node
// automatically.
type Multicast struct {
	phony.Inbox
	core        *core.Core
	log         core.Logger
	sock        *ipv6.PacketConn
	running     atomic.Bool
	_listeners  map[string]*listenerInfo
	_interfaces map[string]*interfaceInfo
	_timer      *time.Timer
	config      struct {
		_groupAddr  GroupAddress
		_interfaces map[MulticastInterface]struct{}
	}
}

func (m *Multicast) _start() error {
	if !m.running.CompareAndSwap(false, true) {
		return fmt.Errorf("multicast module is already started")
	}
	var anyEnabled bool
	for intf := range m.config._interfaces {
		anyEnabled = anyEnabled || intf.Beacon || intf.Listen
	}
	if !anyEnabled {
		m.running.Store(false)
		return nil
	}
	m.log.Debugln("Starting multicast module")
	defer m.log.Debugln("Started multicast module")
	addr, err := net.ResolveUDPAddr("udp", string(m.config._groupAddr))
	if err != nil {
		m.running.Store(false)
		return err
	}
	listenString := fmt.Sprintf("[::]:%v", addr.Port)
	lc := net.ListenConfig{
		Control: m.multicastReuse,
	}
	conn, err := lc.ListenPacket(context.Background(), "udp6", listenString)
	if err != nil {
		m.running.Store(false)
		return err
	}
	m.sock = ipv6.NewPacketConn(conn)
	if err = m.sock.SetControlMessage(ipv6.FlagDst, true); err != nil { // nolint:staticcheck
		// Windows can't set this flag, so we need to handle it in other ways
	}

	go m.listen()
	m.Act(nil, m._multicastStarted)
	m.Act(nil, m._announce)

	return nil
}