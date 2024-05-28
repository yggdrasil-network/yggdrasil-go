package multicast

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"time"

	"github.com/Arceliar/phony"
	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/net/ipv6"
)

// Multicast represents the multicast advertisement and discovery mechanism used
// by Yggdrasil to find peers on the same subnet. When a beacon is received on a
// configured multicast interface, Yggdrasil will attempt to peer with that node
// automatically.
type Multicast struct {
	phony.Inbox
	core        *core.Core
	log         *log.Logger
	sock        *ipv6.PacketConn
	_isOpen     bool
	_listeners  map[string]*listenerInfo
	_interfaces map[string]*interfaceInfo
	_timer      *time.Timer
	config      struct {
		_groupAddr  GroupAddress
		_interfaces map[MulticastInterface]struct{}
	}
}

type interfaceInfo struct {
	iface    net.Interface
	addrs    []net.Addr
	beacon   bool
	listen   bool
	port     uint16
	priority uint8
	password []byte
	hash     []byte
}

type listenerInfo struct {
	listener *core.Listener
	time     time.Time
	interval time.Duration
	port     uint16
}

// Start starts the multicast interface. This launches goroutines which will
// listen for multicast beacons from other hosts and will advertise multicast
// beacons out to the network.
func New(core *core.Core, log *log.Logger, opts ...SetupOption) (*Multicast, error) {
	m := &Multicast{
		core:        core,
		log:         log,
		_listeners:  make(map[string]*listenerInfo),
		_interfaces: make(map[string]*interfaceInfo),
	}
	m.config._interfaces = map[MulticastInterface]struct{}{}
	m.config._groupAddr = GroupAddress("[ff02::114]:9001")
	for _, opt := range opts {
		m._applyOption(opt)
	}
	var err error
	phony.Block(m, func() {
		err = m._start()
	})
	return m, err
}

func (m *Multicast) _start() error {
	if m._isOpen {
		return fmt.Errorf("multicast module is already started")
	}
	var anyEnabled bool
	for intf := range m.config._interfaces {
		anyEnabled = anyEnabled || intf.Beacon || intf.Listen
	}
	if !anyEnabled {
		return nil
	}
	m.log.Debugln("Starting multicast module")
	defer m.log.Debugln("Started multicast module")
	addr, err := net.ResolveUDPAddr("udp", string(m.config._groupAddr))
	if err != nil {
		return err
	}
	listenString := fmt.Sprintf("[::]:%v", addr.Port)
	lc := net.ListenConfig{
		Control: m.multicastReuse,
	}
	conn, err := lc.ListenPacket(context.Background(), "udp6", listenString)
	if err != nil {
		return err
	}
	m.sock = ipv6.NewPacketConn(conn)
	if err = m.sock.SetControlMessage(ipv6.FlagDst, true); err != nil { // nolint:staticcheck
		// Windows can't set this flag, so we need to handle it in other ways
	}

	m._isOpen = true
	go m.listen()
	m.Act(nil, m._multicastStarted)
	m.Act(nil, m._announce)

	return nil
}

// IsStarted returns true if the module has been started.
func (m *Multicast) IsStarted() bool {
	var isOpen bool
	phony.Block(m, func() {
		isOpen = m._isOpen
	})
	return isOpen
}

// Stop stops the multicast module.
func (m *Multicast) Stop() error {
	var err error
	phony.Block(m, func() {
		err = m._stop()
	})
	m.log.Debugln("Stopped multicast module")
	return err
}

func (m *Multicast) _stop() error {
	m.log.Infoln("Stopping multicast module")
	m._isOpen = false
	if m.sock != nil {
		m.sock.Close()
	}
	return nil
}

func (m *Multicast) _updateInterfaces() {
	interfaces := m._getAllowedInterfaces()
	for name, info := range interfaces {
		addrs, err := info.iface.Addrs()
		if err != nil {
			m.log.Warnf("Failed up get addresses for interface %s: %s", name, err)
			delete(interfaces, name)
			continue
		}
		info.addrs = addrs
		interfaces[name] = info
	}
	m._interfaces = interfaces
}

func (m *Multicast) Interfaces() map[string]net.Interface {
	interfaces := make(map[string]net.Interface)
	phony.Block(m, func() {
		for _, info := range m._interfaces {
			interfaces[info.iface.Name] = info.iface
		}
	})
	return interfaces
}

// getAllowedInterfaces returns the currently known/enabled multicast interfaces.
func (m *Multicast) _getAllowedInterfaces() map[string]*interfaceInfo {
	interfaces := make(map[string]*interfaceInfo)
	// Ask the system for network interfaces
	allifaces, err := net.Interfaces()
	if err != nil {
		// Don't panic, since this may be from e.g. too many open files (from too much connection spam)
		// TODO? log something
		return nil
	}
	// Work out which interfaces to announce on
	pk := m.core.PublicKey()
	for _, iface := range allifaces {
		switch {
		case iface.Flags&net.FlagUp == 0:
			continue // Ignore interfaces that are down
		case iface.Flags&net.FlagMulticast == 0:
			continue // Ignore non-multicast interfaces
		case iface.Flags&net.FlagPointToPoint != 0:
			continue // Ignore point-to-point interfaces
		}
		for ifcfg := range m.config._interfaces {
			// Compile each regular expression
			// Does the interface match the regular expression? Store it if so
			if !ifcfg.Beacon && !ifcfg.Listen {
				continue
			}
			if !ifcfg.Regex.MatchString(iface.Name) {
				continue
			}
			hasher, err := blake2b.New512([]byte(ifcfg.Password))
			if err != nil {
				continue
			}
			if n, err := hasher.Write(pk); err != nil {
				continue
			} else if n != ed25519.PublicKeySize {
				continue
			}
			interfaces[iface.Name] = &interfaceInfo{
				iface:    iface,
				beacon:   ifcfg.Beacon,
				listen:   ifcfg.Listen,
				port:     ifcfg.Port,
				priority: ifcfg.Priority,
				password: []byte(ifcfg.Password),
				hash:     hasher.Sum(nil),
			}
			break
		}
	}
	return interfaces
}

func (m *Multicast) AnnounceNow() {
	phony.Block(m, func() {
		if m._timer != nil && !m._timer.Stop() {
			<-m._timer.C
		}
		m.Act(nil, m._announce)
	})
}

func (m *Multicast) _announce() {
	if !m._isOpen {
		return
	}
	m._updateInterfaces()
	groupAddr, err := net.ResolveUDPAddr("udp6", string(m.config._groupAddr))
	if err != nil {
		panic(err)
	}
	destAddr, err := net.ResolveUDPAddr("udp6", string(m.config._groupAddr))
	if err != nil {
		panic(err)
	}
	// There might be interfaces that we configured listeners for but are no
	// longer up - if that's the case then we should stop the listeners
	for name, info := range m._listeners {
		// Prepare our stop function!
		stop := func() {
			info.listener.Close()
			delete(m._listeners, name)
			m.log.Debugln("No longer multicasting on", name)
		}
		// If the interface is no longer visible on the system then stop the
		// listener, as another one will be started further down
		if _, ok := m._interfaces[name]; !ok {
			stop()
			continue
		}
		// It's possible that the link-local listener address has changed so if
		// that is the case then we should clean up the interface listener
		found := false
		listenaddr, err := net.ResolveTCPAddr("tcp6", info.listener.Addr().String())
		if err != nil {
			stop()
			continue
		}
		// Find the interface that matches the listener
		if info, ok := m._interfaces[name]; ok {
			for _, addr := range info.addrs {
				if ip, _, err := net.ParseCIDR(addr.String()); err == nil {
					// Does the interface address match our listener address?
					if ip.Equal(listenaddr.IP) {
						found = true
						break
					}
				}
			}
		}
		// If the address has not been found on the adapter then we should stop
		// and clean up the TCP listener. A new one will be created below if a
		// suitable link-local address is found
		if !found {
			stop()
		}
	}
	// Now that we have a list of valid interfaces from the operating system,
	// we can start checking if we can send multicasts on them
	for _, info := range m._interfaces {
		iface := info.iface
		for _, addr := range info.addrs {
			addrIP, _, _ := net.ParseCIDR(addr.String())
			// Ignore IPv4 addresses
			if addrIP.To4() != nil {
				continue
			}
			// Ignore non-link-local addresses
			if !addrIP.IsLinkLocalUnicast() {
				continue
			}
			if info.listen {
				// Join the multicast group, so we can listen for beacons
				_ = m.sock.JoinGroup(&iface, groupAddr)
			}
			if !info.beacon {
				break // Don't send multicast beacons or accept incoming connections
			}
			// Try and see if we already have a TCP listener for this interface
			var linfo *listenerInfo
			if _, ok := m._listeners[iface.Name]; !ok {
				// No listener was found - let's create one
				v := &url.Values{}
				v.Add("priority", fmt.Sprintf("%d", info.priority))
				v.Add("password", string(info.password))
				u := &url.URL{
					Scheme:   "tls",
					Host:     net.JoinHostPort(addrIP.String(), fmt.Sprintf("%d", info.port)),
					RawQuery: v.Encode(),
				}
				if li, err := m.core.Listen(u, iface.Name); err == nil {
					m.log.Debugln("Started multicasting on", iface.Name)
					// Store the listener so that we can stop it later if needed
					linfo = &listenerInfo{listener: li, time: time.Now(), port: info.port}
					m._listeners[iface.Name] = linfo
				} else {
					m.log.Warnln("Not multicasting on", iface.Name, "due to error:", err)
				}
			} else {
				// An existing listener was found
				linfo = m._listeners[iface.Name]
			}
			// Make sure nothing above failed for some reason
			if linfo == nil {
				continue
			}
			if time.Since(linfo.time) < linfo.interval {
				continue
			}
			addr := linfo.listener.Addr().(*net.TCPAddr)
			adv := multicastAdvertisement{
				MajorVersion: core.ProtocolVersionMajor,
				MinorVersion: core.ProtocolVersionMinor,
				PublicKey:    m.core.PublicKey(),
				Port:         uint16(addr.Port),
				Hash:         info.hash,
			}
			msg, err := adv.MarshalBinary()
			if err != nil {
				continue
			}
			destAddr.Zone = iface.Name
			if _, err = m.sock.WriteTo(msg, nil, destAddr); err != nil {
				m.log.Warn("Failed to send multicast beacon:", err)
			}
			if linfo.interval.Seconds() < 15 {
				linfo.interval += time.Second
			}
			linfo.time = time.Now()
			break
		}
	}
	annInterval := time.Second + time.Microsecond*(time.Duration(rand.Intn(1048576))) // Randomize delay
	m._timer = time.AfterFunc(annInterval, func() {
		m.Act(nil, m._announce)
	})
}

func (m *Multicast) listen() {
	groupAddr, err := net.ResolveUDPAddr("udp6", string(m.config._groupAddr))
	if err != nil {
		panic(err)
	}
	bs := make([]byte, 2048)
	hb := make([]byte, 0, blake2b.Size) // Reused to reduce hash allocations
	for {
		n, rcm, fromAddr, err := m.sock.ReadFrom(bs)
		if err != nil {
			if !m.IsStarted() {
				return
			}
			panic(err)
		}
		if rcm != nil {
			// Windows can't set the flag needed to return a non-nil value here
			// So only make these checks if we get something useful back
			// TODO? Skip them always, I'm not sure if they're really needed...
			if !rcm.Dst.IsLinkLocalMulticast() {
				continue
			}
			if !rcm.Dst.Equal(groupAddr.IP) {
				continue
			}
		}
		var adv multicastAdvertisement
		if err := adv.UnmarshalBinary(bs[:n]); err != nil {
			continue
		}
		switch {
		case adv.MajorVersion != core.ProtocolVersionMajor:
			continue
		case adv.MinorVersion != core.ProtocolVersionMinor:
			continue
		case adv.PublicKey.Equal(m.core.PublicKey()):
			continue
		}
		from := fromAddr.(*net.UDPAddr)
		from.Port = int(adv.Port)
		var interfaces map[string]*interfaceInfo
		phony.Block(m, func() {
			interfaces = m._interfaces
		})
		if info, ok := interfaces[from.Zone]; ok && info.listen {
			hasher, err := blake2b.New512(info.password)
			if err != nil {
				continue
			}
			if n, err := hasher.Write(adv.PublicKey); err != nil {
				continue
			} else if n != ed25519.PublicKeySize {
				continue
			}
			if !bytes.Equal(hasher.Sum(hb[:0]), adv.Hash) {
				continue
			}
			v := &url.Values{}
			v.Add("key", hex.EncodeToString(adv.PublicKey))
			v.Add("priority", fmt.Sprintf("%d", info.priority))
			v.Add("password", string(info.password))
			u := &url.URL{
				Scheme:   "tls",
				Host:     from.String(),
				RawQuery: v.Encode(),
			}
			if err := m.core.CallPeer(u, from.Zone); err != nil {
				m.log.Debugln("Call from multicast failed:", err)
			}
		}
	}
}
