//go:build plan9

package multicast

// Plan 9 multicast beacon socket.
//
// golang.org/x/net/ipv6 is stubbed out on Plan 9 and the kernel has no
// BSD-style multicast socket options, so the beacon socket is driven
// directly through /net/udp. Per ip(3):
//
//   - open /net/udp/clone to reserve a UDP conversation; the returned fd
//     is that conversation's ctl file and reading it yields the number N.
//   - write "announce *!port" then "headers" to ctl. In headers mode each
//     read/write on /net/udp/N/data is prefixed with a 52-byte Udphdr
//     (raddr[16] laddr[16] ifcaddr[16] rport[2] lport[2]); ports are in
//     network (big-endian) order (the kernel reads them with nhgets and
//     writes them with hnputs).
//   - write "addmulti <ifc-addr> <mcast-addr>" to ctl to join a multicast
//     group on the interface that owns <ifc-addr>.
//
// The Multicast struct and _start (socket creation) are plan9-specific;
// the announce/listen/stop logic in multicast.go is shared and calls the
// sock methods below. plan9Sock deliberately mirrors the subset of
// *ipv6.PacketConn's method signatures that multicast.go uses
// (JoinGroup, WriteTo, ReadFrom, Close), so the shared code compiles and
// runs unchanged on both sides. ReadFrom returns an *ipv6.ControlMessage
// with Dst set to the received packet's destination (the multicast group)
// so the shared listen() can apply the same IsLinkLocalMulticast / Equal
// checks it does on other platforms.

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Arceliar/phony"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"golang.org/x/net/ipv6"
)

const udpNetmtpt = "/net"

// udphdrLen is the size of the Udphdr prefix in headers mode.
const udphdrLen = 16 + 16 + 16 + 2 + 2

// Multicast represents the multicast advertisement and discovery mechanism used
// by Yggdrasil to find peers on the same subnet. On Plan 9 the socket is a
// native /net/udp conversation (plan9Sock) instead of *ipv6.PacketConn.
type Multicast struct {
	phony.Inbox
	core        *core.Core
	log         core.Logger
	sock        *plan9Sock
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
	if m.sock, err = newPlan9Sock(m.log, addr.Port); err != nil {
		m.running.Store(false)
		return err
	}

	go m.listen()
	m.Act(nil, m._multicastStarted)
	m.Act(nil, m._announce)

	return nil
}

// _multicastStarted is a no-op on Plan 9 (no AWDL/extra discovery to drive).
func (m *Multicast) _multicastStarted() {}

// plan9Sock implements the subset of *ipv6.PacketConn that multicast.go uses,
// over a Plan 9 "headers"-mode UDP conversation.
type plan9Sock struct {
	log  core.Logger
	ctl  *os.File
	data *os.File
	port int

	mu       sync.RWMutex
	nameAddr map[string]net.IP // interface name -> link-local source address (for WriteTo)
	addrName map[string]string // any local address string -> interface name (for ReadFrom zone)
}

func newPlan9Sock(log core.Logger, port int) (*plan9Sock, error) {
	// Reclaim any UDP conversation still announced on our port from a
	// previous instance that didn't shut down cleanly. On Plan 9 a hard
	// killed process can leave its announced UDP conv behind (state Open),
	// which would make our "announce *!port" fail with "address in use".
	cleanupStaleUDP(log, port)

	ctl, err := os.OpenFile(udpNetmtpt+"/udp/clone", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s/udp/clone: %w", udpNetmtpt, err)
	}
	// A single read on the clone fd yields the conversation number.
	buf := make([]byte, 64)
	n, err := ctl.Read(buf)
	if err != nil {
		ctl.Close()
		return nil, fmt.Errorf("reading udp conv number: %w", err)
	}
	num := strings.TrimSpace(string(buf[:n]))
	if num == "" {
		ctl.Close()
		return nil, fmt.Errorf("udp clone returned empty number")
	}

	// Announce on the port. A port just freed by a reclaim — or still
	// held for a moment by a previous instance that is shutting down —
	// may not be reusable instantly, so retry briefly, re-running the
	// reclaim between attempts.
	var announceErr error
	for attempt := 0; attempt < 5; attempt++ {
		if _, err := fmt.Fprintf(ctl, "announce *!%d\n", port); err == nil {
			announceErr = nil
			break
		}
		announceErr = err
		cleanupStaleUDP(log, port)
		time.Sleep(200 * time.Millisecond)
	}
	if announceErr != nil {
		ctl.Close()
		return nil, fmt.Errorf("udp announce: %w", announceErr)
	}
	if _, err := ctl.WriteString("headers\n"); err != nil {
		ctl.Close()
		return nil, fmt.Errorf("udp headers: %w", err)
	}

	data, err := os.OpenFile(udpNetmtpt+"/udp/"+num+"/data", os.O_RDWR, 0)
	if err != nil {
		ctl.Close()
		return nil, fmt.Errorf("opening %s/udp/%s/data: %w", udpNetmtpt, num, err)
	}

	log.Debugf("Plan 9 multicast socket: udp/%s on port %d", num, port)
	return &plan9Sock{
		log:      log,
		ctl:      ctl,
		data:     data,
		port:     port,
		nameAddr: make(map[string]net.IP),
		addrName: make(map[string]string),
	}, nil
}

// cleanupStaleUDP hangs up any pre-existing UDP conversation still
// announced on the given port. A hard-killed yggdrasil can leave its
// multicast UDP conv behind (it is not reaped when the process dies),
// which would otherwise make our announce fail with "address in use".
// /net/udp/N/local reports "laddr!lport"; an announced conv shows our
// port as the local port.
func cleanupStaleUDP(log core.Logger, port int) {
	entries, err := os.ReadDir(udpNetmtpt + "/udp")
	if err != nil {
		return
	}
	suffix := fmt.Sprintf("!%d", port)
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // skip clone, stats, …
		}
		local, err := os.ReadFile(udpNetmtpt + "/udp/" + e.Name() + "/local")
		if err != nil {
			continue
		}
		if !strings.HasSuffix(strings.TrimSpace(string(local)), suffix) {
			continue
		}
		ctl, err := os.OpenFile(udpNetmtpt+"/udp/"+e.Name()+"/ctl", os.O_WRONLY, 0)
		if err != nil {
			continue
		}
		log.Warnf("Plan 9: reclaiming stale multicast UDP conv udp/%s (port %d)", e.Name(), port)
		_, _ = ctl.WriteString("hangup\n")
		ctl.Close()
	}
}

// linkLocalAddr returns the interface's IPv6 link-local address.
func linkLocalAddr(ifi *net.Interface) (net.IP, error) {
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err != nil || ip.To4() != nil || !ip.IsLinkLocalUnicast() {
			continue
		}
		return ip, nil
	}
	return nil, fmt.Errorf("no IPv6 link-local address on interface %s", ifi.Name)
}

// JoinGroup joins the multicast group on the given interface. Plan 9
// maps this to "addmulti <ifc-addr> <mcast-addr>" on the UDP conv ctl.
// It also records the interface's local addresses so WriteTo can pick a
// source and ReadFrom can recover the receiving interface's name.
func (s *plan9Sock) JoinGroup(ifi *net.Interface, group net.Addr) error {
	ll, err := linkLocalAddr(ifi)
	if err != nil {
		return err
	}
	gaddr, ok := group.(*net.UDPAddr)
	if !ok {
		return fmt.Errorf("unexpected group address type: %T", group)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.ctl, "addmulti %s %s\n", ll.String(), gaddr.IP.String()); err != nil {
		return fmt.Errorf("addmulti: %w", err)
	}
	s.nameAddr[ifi.Name] = ll
	// Record every local address of the interface so the ifcaddr returned
	// on receive (which may be any local address, not just link-local)
	// maps back to the interface name.
	addrs, err := ifi.Addrs()
	if err == nil {
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil || ip.To4() != nil {
				continue
			}
			s.addrName[ip.String()] = ifi.Name
		}
	}
	return nil
}

// WriteTo sends b as a single multicast datagram to dst. The control
// message is ignored (no per-packet socket options on Plan 9). The
// source address is the interface's link-local address, selected by
// dst.Zone (the interface name set by the announce loop).
func (s *plan9Sock) WriteTo(b []byte, _ *ipv6.ControlMessage, dst net.Addr) (int, error) {
	u, ok := dst.(*net.UDPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected destination type: %T", dst)
	}
	s.mu.RLock()
	src := s.nameAddr[u.Zone]
	s.mu.RUnlock()

	hdr := make([]byte, udphdrLen)
	copy(hdr[0:16], u.IP.To16()) // raddr
	if src != nil {              // laddr (source); zero lets the kernel pick
		copy(hdr[16:32], src.To16())
	}
	// ifcaddr [32:48] is ignored on write.
	binary.BigEndian.PutUint16(hdr[48:50], uint16(u.Port)) // rport
	// lport [50:52] is ignored (overridden by the announced port).

	msg := append(hdr, b...)
	if _, err := s.data.Write(msg); err != nil {
		return 0, err
	}
	return len(b), nil
}

// ReadFrom receives one datagram. It returns the payload (copied to the
// front of b), a ControlMessage whose Dst is the packet's destination
// (the multicast group), and the source address with its Zone set to the
// receiving interface's name — matching what the shared listen() expects.
func (s *plan9Sock) ReadFrom(b []byte) (int, *ipv6.ControlMessage, net.Addr, error) {
	n, err := s.data.Read(b)
	if err != nil {
		return 0, nil, nil, err
	}
	if n < udphdrLen {
		return 0, nil, nil, fmt.Errorf("short udp read: %d bytes", n)
	}
	// Copy the header fields out before moving the payload to the front
	// of b (the move would overwrite them for large payloads).
	raddr := append(net.IP(nil), b[0:16]...)
	laddr := append(net.IP(nil), b[16:32]...)
	ifcaddr := append(net.IP(nil), b[32:48]...)
	rport := binary.BigEndian.Uint16(b[48:50])

	payload := b[udphdrLen:n]
	copy(b, payload)

	from := &net.UDPAddr{IP: raddr, Port: int(rport)}
	s.mu.RLock()
	from.Zone = s.addrName[ifcaddr.String()]
	s.mu.RUnlock()

	return len(payload), &ipv6.ControlMessage{Dst: laddr}, from, nil
}

// Close tears down the UDP conversation, which also leaves the joined
// multicast groups.
func (s *plan9Sock) Close() error {
	_, _ = s.ctl.WriteString("hangup\n")
	_ = s.data.Close()
	return s.ctl.Close()
}