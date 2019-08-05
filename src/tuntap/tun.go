package tuntap

// This manages the tun driver to send/recv packets to/from applications

// TODO: Crypto-key routing support
// TODO: Set MTU of session properly
// TODO: Reject packets that exceed session MTU with ICMPv6 for PMTU Discovery
// TODO: Connection timeouts (call Conn.Close() when we want to time out)
// TODO: Don't block in reader on writes that are pending searches

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/gologme/log"
	"github.com/yggdrasil-network/water"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

const tun_IPv6_HEADER_LENGTH = 40
const tun_ETHER_HEADER_LENGTH = 14

// TunAdapter represents a running TUN/TAP interface and extends the
// yggdrasil.Adapter type. In order to use the TUN/TAP adapter with Yggdrasil,
// you should pass this object to the yggdrasil.SetRouterAdapter() function
// before calling yggdrasil.Start().
type TunAdapter struct {
	config       *config.NodeState
	log          *log.Logger
	reconfigure  chan chan error
	listener     *yggdrasil.Listener
	dialer       *yggdrasil.Dialer
	addr         address.Address
	subnet       address.Subnet
	ckr          cryptokey
	icmpv6       ICMPv6
	mtu          int
	iface        *water.Interface
	send         chan []byte
	mutex        sync.RWMutex // Protects the below
	addrToConn   map[address.Address]*tunConn
	subnetToConn map[address.Subnet]*tunConn
	dials        map[crypto.NodeID][][]byte // Buffer of packets to send after dialing finishes
	isOpen       bool
}

// Gets the maximum supported MTU for the platform based on the defaults in
// defaults.GetDefaults().
func getSupportedMTU(mtu int) int {
	if mtu > defaults.GetDefaults().MaximumIfMTU {
		return defaults.GetDefaults().MaximumIfMTU
	}
	return mtu
}

// Name returns the name of the adapter, e.g. "tun0". On Windows, this may
// return a canonical adapter name instead.
func (tun *TunAdapter) Name() string {
	return tun.iface.Name()
}

// MTU gets the adapter's MTU. This can range between 1280 and 65535, although
// the maximum value is determined by your platform. The returned value will
// never exceed that of MaximumMTU().
func (tun *TunAdapter) MTU() int {
	return getSupportedMTU(tun.mtu)
}

// IsTAP returns true if the adapter is a TAP adapter (Layer 2) or false if it
// is a TUN adapter (Layer 3).
func (tun *TunAdapter) IsTAP() bool {
	return tun.iface.IsTAP()
}

// DefaultName gets the default TUN/TAP interface name for your platform.
func DefaultName() string {
	return defaults.GetDefaults().DefaultIfName
}

// DefaultMTU gets the default TUN/TAP interface MTU for your platform. This can
// be as high as MaximumMTU(), depending on platform, but is never lower than 1280.
func DefaultMTU() int {
	return defaults.GetDefaults().DefaultIfMTU
}

// DefaultIsTAP returns true if the default adapter mode for the current
// platform is TAP (Layer 2) and returns false for TUN (Layer 3).
func DefaultIsTAP() bool {
	return defaults.GetDefaults().DefaultIfTAPMode
}

// MaximumMTU returns the maximum supported TUN/TAP interface MTU for your
// platform. This can be as high as 65535, depending on platform, but is never
// lower than 1280.
func MaximumMTU() int {
	return defaults.GetDefaults().MaximumIfMTU
}

// Init initialises the TUN/TAP module. You must have acquired a Listener from
// the Yggdrasil core before this point and it must not be in use elsewhere.
func (tun *TunAdapter) Init(config *config.NodeState, log *log.Logger, listener *yggdrasil.Listener, dialer *yggdrasil.Dialer) {
	tun.config = config
	tun.log = log
	tun.listener = listener
	tun.dialer = dialer
	tun.addrToConn = make(map[address.Address]*tunConn)
	tun.subnetToConn = make(map[address.Subnet]*tunConn)
	tun.dials = make(map[crypto.NodeID][][]byte)
}

// Start the setup process for the TUN/TAP adapter. If successful, starts the
// read/write goroutines to handle packets on that interface.
func (tun *TunAdapter) Start() error {
	current := tun.config.GetCurrent()
	if tun.config == nil || tun.listener == nil || tun.dialer == nil {
		return errors.New("No configuration available to TUN/TAP")
	}
	var boxPub crypto.BoxPubKey
	boxPubHex, err := hex.DecodeString(current.EncryptionPublicKey)
	if err != nil {
		return err
	}
	copy(boxPub[:], boxPubHex)
	nodeID := crypto.GetNodeID(&boxPub)
	tun.addr = *address.AddrForNodeID(nodeID)
	tun.subnet = *address.SubnetForNodeID(nodeID)
	tun.mtu = current.IfMTU
	ifname := current.IfName
	iftapmode := current.IfTAPMode
	addr := fmt.Sprintf("%s/%d", net.IP(tun.addr[:]).String(), 8*len(address.GetPrefix())-1)
	if ifname != "none" {
		if err := tun.setup(ifname, iftapmode, addr, tun.mtu); err != nil {
			return err
		}
	}
	if ifname == "none" || ifname == "dummy" {
		tun.log.Debugln("Not starting TUN/TAP as ifname is none or dummy")
		return nil
	}
	tun.mutex.Lock()
	tun.isOpen = true
	tun.send = make(chan []byte, 32) // TODO: is this a sensible value?
	tun.reconfigure = make(chan chan error)
	tun.mutex.Unlock()
	go func() {
		for {
			e := <-tun.reconfigure
			e <- nil
		}
	}()
	go tun.handler()
	go tun.reader()
	go tun.writer()
	tun.icmpv6.Init(tun)
	if iftapmode {
		go tun.icmpv6.Solicit(tun.addr)
	}
	tun.ckr.init(tun)
	return nil
}

// Start the setup process for the TUN/TAP adapter. If successful, starts the
// read/write goroutines to handle packets on that interface.
func (tun *TunAdapter) Stop() error {
	tun.isOpen = false
	// TODO: we have nothing that cleanly stops all the various goroutines opened
	// by TUN/TAP, e.g. readers/writers, sessions
	tun.iface.Close()
	return nil
}

// UpdateConfig updates the TUN/TAP module with the provided config.NodeConfig
// and then signals the various module goroutines to reconfigure themselves if
// needed.
func (tun *TunAdapter) UpdateConfig(config *config.NodeConfig) {
	tun.log.Debugln("Reloading TUN/TAP configuration...")

	tun.config.Replace(*config)

	errors := 0

	components := []chan chan error{
		tun.reconfigure,
		tun.ckr.reconfigure,
	}

	for _, component := range components {
		response := make(chan error)
		component <- response
		if err := <-response; err != nil {
			tun.log.Errorln(err)
			errors++
		}
	}

	if errors > 0 {
		tun.log.Warnln(errors, "TUN/TAP module(s) reported errors during configuration reload")
	} else {
		tun.log.Infoln("TUN/TAP configuration reloaded successfully")
	}
}

func (tun *TunAdapter) handler() error {
	for {
		// Accept the incoming connection
		conn, err := tun.listener.Accept()
		if err != nil {
			tun.log.Errorln("TUN/TAP connection accept error:", err)
			return err
		}
		if _, err := tun.wrap(conn); err != nil {
			// Something went wrong when storing the connection, typically that
			// something already exists for this address or subnet
			tun.log.Debugln("TUN/TAP handler wrap:", err)
		}
	}
}

func (tun *TunAdapter) wrap(conn *yggdrasil.Conn) (c *tunConn, err error) {
	// Prepare a session wrapper for the given connection
	s := tunConn{
		tun:   tun,
		conn:  conn,
		send:  make(chan []byte, 32), // TODO: is this a sensible value?
		stop:  make(chan struct{}),
		alive: make(chan struct{}, 1),
	}
	c = &s
	// Get the remote address and subnet of the other side
	remoteNodeID := conn.RemoteAddr()
	s.addr = *address.AddrForNodeID(&remoteNodeID)
	s.snet = *address.SubnetForNodeID(&remoteNodeID)
	// Work out if this is already a destination we already know about
	tun.mutex.Lock()
	defer tun.mutex.Unlock()
	atc, aok := tun.addrToConn[s.addr]
	stc, sok := tun.subnetToConn[s.snet]
	// If we know about a connection for this destination already then assume it
	// is no longer valid and close it
	if aok {
		atc._close_nomutex()
		err = errors.New("replaced connection for address")
	} else if sok {
		stc._close_nomutex()
		err = errors.New("replaced connection for subnet")
	}
	// Save the session wrapper so that we can look it up quickly next time
	// we receive a packet through the interface for this address
	tun.addrToConn[s.addr] = &s
	tun.subnetToConn[s.snet] = &s
	// Start the connection goroutines
	go s.reader()
	go s.writer()
	go s.checkForTimeouts()
	// Return
	return c, err
}
