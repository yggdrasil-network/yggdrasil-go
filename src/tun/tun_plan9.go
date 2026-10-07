//go:build plan9

package tun

// Plan 9 platform specific tun parts.
//
// Plan 9 has no in-kernel TUN/TAP device. The equivalent is a "packet"
// ip interface (ipifc) created with the "bind pkt" control message —
// see ip(3). The kernel hands IP packets to and from a user program via
// the interface's data file (/net/ipifc/N/data): a read returns the
// next IP packet the kernel routed to this interface (i.e. traffic the
// local stack wants to send over the overlay); a write injects an IP
// packet into the local stack (i.e. traffic arriving from the overlay).
// Each 9P read/write transfers exactly one IP packet, so BatchSize is 1.
//
// This is the same mechanism used by 6in4(8) and ppp(8) to mediate IP
// packets between the kernel and a user-space transport. yggdrasil's
// TUN read/write goroutines map onto it directly: the packet format on
// both sides is raw IPv6 (no L2 header), which is exactly what the Plan 9
// IP stack deals in and what ipv6rwc expects.

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	wgtun "golang.zx2c4.com/wireguard/tun"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// netmtpt is the mount point of the IP stack we bind the packet
// interface on. Plan 9 defaults to /net; a process with a private
// namespace may have it elsewhere, but /net is the standard.
const netmtpt = "/net"

// plan9Tun implements wgtun.Device over a Plan 9 "pkt" ip interface.
type plan9Tun struct {
	log    core.Logger
	num    string // ipifc number, as read from the clone ctl file
	name   string // friendly name, e.g. "ipifc/2"
	mtu    int    // configured MTU in bytes
	ctl    *os.File // /net/ipifc/N/ctl (the fd returned by opening clone)
	data   *os.File // /net/ipifc/N/data — raw IP packet read/write
	events chan wgtun.Event

	mu     sync.Mutex
	closed bool
}

// Configures the TUN adapter with the correct IPv6 address and MTU.
// ifname is ignored: Plan 9 ip interfaces are numbered, not named, and
// the number is assigned by the kernel when /net/ipifc/clone is opened.
func (tun *TunAdapter) setup(ifname string, addr string, mtu uint64) error {
	dev, err := newPlan9Tun(tun.log, addr, mtu)
	if err != nil {
		return fmt.Errorf("failed to create TUN: %w", err)
	}
	tun.iface = dev
	if m, err := dev.MTU(); err == nil {
		tun.mtu = getSupportedMTU(uint64(m))
	} else {
		tun.mtu = getSupportedMTU(mtu)
	}
	if addr != "" {
		return tun.setupAddress(addr)
	}
	return nil
}

// Configures the "pkt" adapter from an existing file descriptor.
// Not supported on Plan 9 — there is no foreign TUN fd to adopt.
func (tun *TunAdapter) setupFD(fd int32, addr string, mtu uint64) error {
	return fmt.Errorf("setup via FD not supported on this platform")
}

// Configures the TUN adapter with the correct IPv6 address. The address
// string passed in is "<ip>/<prefix>", e.g. "0200:abcd::1234/7". The /7
// prefix is yggdrasil's whole overlay range (address.GetPrefix); adding
// it with that mask makes 0200::/7 on-link via this interface, so the
// kernel routes all overlay traffic into the tunnel — mirroring what
// Linux does via netlink AddrAdd with the /7 prefix.
func (tun *TunAdapter) setupAddress(addr string) error {
	dev, ok := tun.iface.(*plan9Tun)
	if !ok {
		return fmt.Errorf("unexpected TUN device type on plan9")
	}
	ip, mask, err := parseAddrMask(addr)
	if err != nil {
		return fmt.Errorf("couldn't parse address %q: %w", addr, err)
	}
	// "add local mask" — see ip(3). Adding the address with the /7 mask
	// installs a connected route for 0200::/7 on this interface.
	if _, err := fmt.Fprintf(dev.ctl, "add %s %s\n", ip, mask); err != nil {
		return fmt.Errorf("failed to add address to ipifc: %w", err)
	}
	tun.log.Infof("Interface name: %s", tun.Name())
	tun.log.Infof("Interface IPv6: %s", addr)
	tun.log.Infof("Interface MTU: %d", tun.mtu)
	return nil
}

// newPlan9Tun reserves an ip interface, binds it as a packet interface,
// opens its data file, and configures the MTU.
func newPlan9Tun(log core.Logger, addr string, mtu uint64) (*plan9Tun, error) {
	// Reclaim any packet interface left behind by a previous instance
	// that didn't shut down cleanly. On Plan 9 a pkt ipifc is not
	// destroyed when its owning process dies (or is hard-killed); it
	// lingers on the IP stack with its address and route until unbound.
	cleanupStaleIfaces(log, addr)

	// Opening clone reserves an interface; the returned fd is the ctl
	// file of the newly allocated ipifc. Reading it yields the number.
	clone := netmtpt + "/ipifc/clone"
	ctl, err := os.OpenFile(clone, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", clone, err)
	}
	// A single read on the clone fd yields the interface number (this is
	// how libc and Go's own plan9 net driver read it; io.ReadAll could
	// block if the ctl file does not return EOF after the number).
	numBuf := make([]byte, 64)
	n, err := ctl.Read(numBuf)
	if err != nil {
		ctl.Close()
		return nil, fmt.Errorf("reading ipifc number: %w", err)
	}
	num := strings.TrimSpace(string(numBuf[:n]))
	if num == "" {
		ctl.Close()
		return nil, errors.New("ipifc clone returned empty number")
	}

	// Bind as a packet interface: the kernel will exchange IP packets
	// with us via the data file.
	if _, err := ctl.WriteString("bind pkt\n"); err != nil {
		ctl.Close()
		return nil, fmt.Errorf("bind pkt: %w", err)
	}

	// Open the data file we'll read/write IP packets through.
	dataPath := netmtpt + "/ipifc/" + num + "/data"
	data, err := os.OpenFile(dataPath, os.O_RDWR, 0)
	if err != nil {
		ctl.WriteString("unbind\n")
		ctl.Close()
		return nil, fmt.Errorf("opening %s: %w", dataPath, err)
	}

	dev := &plan9Tun{
		log:    log,
		num:    num,
		name:   "ipifc/" + num,
		ctl:    ctl,
		data:   data,
		events: make(chan wgtun.Event, 8),
	}

	// Determine the medium's max MTU from the status file and configure
	// the MTU, clamped to what the medium supports. Packet media default
	// to 4096; the status file's first line is "device maxmtu".
	maxmtu := dev.maxMTU()
	want := int(mtu)
	if maxmtu > 0 && want > maxmtu {
		want = maxmtu
	}
	if want < 1280 {
		want = 1280
	}
	if _, err := fmt.Fprintf(ctl, "mtu %d\n", want); err != nil {
		// Non-fatal: the medium's default MTU will be used.
		dev.mtu = maxmtu
	} else {
		dev.mtu = want
	}
	if dev.mtu == 0 {
		dev.mtu = 4096 // packet media default per ip(3)
	}

	return dev, nil
}

// maxMTU reads the interface's status file and returns the maxmtu field
// (second whitespace-separated field of the first line), or 0 on error.
func (t *plan9Tun) maxMTU() int {
	f, err := os.Open(netmtpt + "/ipifc/" + t.num + "/status")
	if err != nil {
		return 0
	}
	defer f.Close()
	st, err := io.ReadAll(f)
	if err != nil || len(st) == 0 {
		return 0
	}
	fields := strings.Fields(string(st))
	if len(fields) < 2 {
		return 0
	}
	m, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return m
}

// parseAddrMask splits a "ip/prefix" CIDR string into the host IP and
// the netmask corresponding to the prefix length.
func parseAddrMask(addr string) (ip, mask string, err error) {
	i := strings.LastIndex(addr, "/")
	if i < 0 {
		return "", "", errors.New("missing prefix length")
	}
	ip = addr[:i]
	_, ipnet, err := net.ParseCIDR(addr)
	if err != nil {
		return "", "", err
	}
	return ip, net.IP(ipnet.Mask).String(), nil
}

// cleanupStaleIfaces unbinds any pre-existing ipifc that already carries
// this node's yggdrasil address. On Plan 9 a pkt ipifc is not reaped when
// its owning process dies or is hard-killed (unlike a Linux tun device);
// it persists on the IP stack with its 0200::/7 address and route until
// explicitly unbound. Reclaiming it here lets yggdrasil restart cleanly
// after an unclean shutdown without leaving a dead route in place or
// failing to add the address (it would already be assigned).
func cleanupStaleIfaces(log core.Logger, addr string) {
	ip := ipFromCIDR(addr)
	if ip == nil {
		return
	}
	entries, err := os.ReadDir(netmtpt + "/ipifc")
	if err != nil {
		return
	}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // skip clone, stats, …
		}
		st, err := os.ReadFile(netmtpt + "/ipifc/" + e.Name() + "/status")
		if err != nil {
			continue
		}
		if !statusHasAddr(string(st), ip) {
			continue
		}
		ctl, err := os.OpenFile(netmtpt+"/ipifc/"+e.Name()+"/ctl", os.O_WRONLY, 0)
		if err != nil {
			continue
		}
		log.Warnf("Plan 9: reclaiming stale yggdrasil interface ipifc/%s (address %s)", e.Name(), ip.String())
		_, _ = ctl.WriteString("unbind\n")
		ctl.Close()
	}
}

// ipFromCIDR extracts the host IP from a "ip/prefix" string.
func ipFromCIDR(addr string) net.IP {
	i := strings.LastIndex(addr, "/")
	if i < 0 {
		return net.ParseIP(addr)
	}
	return net.ParseIP(addr[:i])
}

// statusHasAddr reports whether an ipifc status blob lists the given
// address. The status file's first line is a header; each subsequent
// line begins with an IP address (see ip(3)).
func statusHasAddr(status string, ip net.IP) bool {
	lines := strings.Split(status, "\n")
	for _, line := range lines[1:] { // skip the header line
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if a := net.ParseIP(fields[0]); a != nil && a.Equal(ip) {
			return true
		}
	}
	return false
}

// --- wgtun.Device interface ---

// Name returns the ip interface identifier, e.g. "ipifc/2".
func (t *plan9Tun) Name() (string, error) {
	return t.name, nil
}

// MTU returns the configured MTU in bytes.
func (t *plan9Tun) MTU() (int, error) {
	if t.mtu > 0 {
		return t.mtu, nil
	}
	return 0, errors.New("MTU not set")
}

// Events returns the interface event channel. yggdrasil does not drain
// it; we never send on it.
func (t *plan9Tun) Events() <-chan wgtun.Event {
	return t.events
}

// File is part of the interface; unused by yggdrasil.
func (t *plan9Tun) File() *os.File {
	return nil
}

// BatchSize is 1: each 9P read/write on the data file transfers a
// single IP packet.
func (t *plan9Tun) BatchSize() int {
	return 1
}

// Read receives one IP packet the kernel routed to this interface into
// bufs[0][offset:], recording its length in sizes[0]. It blocks until a
// packet is available.
func (t *plan9Tun) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	if len(bufs) == 0 {
		return 0, errors.New("no buffers")
	}
	dst := bufs[0]
	if offset >= len(dst) {
		return 0, errors.New("offset beyond buffer")
	}
	n, err := t.data.Read(dst[offset:])
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	sizes[0] = n
	return 1, nil
}

// Write injects each IP packet in bufs (starting at offset) into the
// local stack via the data file.
func (t *plan9Tun) Write(bufs [][]byte, offset int) (int, error) {
	written := 0
	for _, b := range bufs {
		if offset >= len(b) {
			continue
		}
		pkt := b[offset:]
		if len(pkt) == 0 {
			continue
		}
		if _, err := t.data.Write(pkt); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// Close unbinds the interface and releases the data and control files.
func (t *plan9Tun) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()
	// "unbind" disassociates the medium and all addresses from the
	// interface, tearing down what bind/add established.
	_, _ = t.ctl.WriteString("unbind\n")
	_ = t.data.Close()
	return t.ctl.Close()
}