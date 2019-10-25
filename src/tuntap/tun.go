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

	//"sync"

	"github.com/Arceliar/phony"
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
	writer      tunWriter
	reader      tunReader
	config      *config.NodeState
	log         *log.Logger
	reconfigure chan chan error
	listener    *yggdrasil.Listener
	dialer      *yggdrasil.Dialer
	addr        address.Address
	subnet      address.Subnet
	ckr         cryptokey
	icmpv6      ICMPv6
	mtu         int
	iface       *water.Interface
	phony.Inbox // Currently only used for _handlePacket from the reader, TODO: all the stuff that currently needs a mutex below
	//mutex        sync.RWMutex // Protects the below
	addrToConn   map[address.Address]*tunConn
	subnetToConn map[address.Subnet]*tunConn
	dials        map[string][][]byte // Buffer of packets to send after dialing finishes
	isOpen       bool
}

type TunOptions struct {
	Listener *yggdrasil.Listener
	Dialer   *yggdrasil.Dialer
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
func (tun *TunAdapter) Init(core *yggdrasil.Core, config *config.NodeState, log *log.Logger, options interface{}) error {
	tunoptions, ok := options.(TunOptions)
	if !ok {
		return fmt.Errorf("invalid options supplied to TunAdapter module")
	}
	tun.config = config
	tun.log = log
	tun.listener = tunoptions.Listener
	tun.dialer = tunoptions.Dialer
	tun.addrToConn = make(map[address.Address]*tunConn)
	tun.subnetToConn = make(map[address.Subnet]*tunConn)
	tun.dials = make(map[string][][]byte)
	tun.writer.tun = tun
	tun.reader.tun = tun
	return nil
}

// Start the setup process for the TUN/TAP adapter. If successful, starts the
// reader actor to handle packets on that interface.
func (tun *TunAdapter) Start() error {
	var err error
	phony.Block(tun, func() {
		err = tun._start()
	})
	return err
}

func (tun *TunAdapter) _start() error {
	if tun.isOpen {
		return errors.New("TUN/TAP module is already started")
	}
	current := tun.config.GetCurrent()
	if tun.config == nil || tun.listener == nil || tun.dialer == nil {
		return errors.New("no configuration available to TUN/TAP")
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
	tun.isOpen = true
	go tun.handler()
	tun.reader.Act(nil, tun.reader._read) // Start the reader
	tun.icmpv6.Init(tun)
	if iftapmode {
		go tun.icmpv6.Solicit(tun.addr)
	}
	tun.ckr.init(tun)
	return nil
}

// IsStarted returns true if the module has been started.
func (tun *TunAdapter) IsStarted() bool {
	var isOpen bool
	phony.Block(tun, func() {
		isOpen = tun.isOpen
	})
	return isOpen
}

// Start the setup process for the TUN/TAP adapter. If successful, starts the
// read/write goroutines to handle packets on that interface.
func (tun *TunAdapter) Stop() error {
	var err error
	phony.Block(tun, func() {
		err = tun._stop()
	})
	return err
}

func (tun *TunAdapter) _stop() error {
	tun.isOpen = false
	// by TUN/TAP, e.g. readers/writers, sessions
	if tun.iface != nil {
		// Just in case we failed to start up the iface for some reason, this can apparently happen on Windows
		tun.iface.Close()
	}
	return nil
}

// UpdateConfig updates the TUN/TAP module with the provided config.NodeConfig
// and then signals the various module goroutines to reconfigure themselves if
// needed.
func (tun *TunAdapter) UpdateConfig(config *config.NodeConfig) {
	tun.log.Debugln("Reloading TUN/TAP configuration...")

	// Replace the active configuration with the supplied one
	tun.config.Replace(*config)

	// Notify children about the configuration change
	tun.Act(nil, tun.ckr.configure)
}

func (tun *TunAdapter) handler() error {
	for {
		// Accept the incoming connection
		conn, err := tun.listener.Accept()
		if err != nil {
			tun.log.Errorln("TUN/TAP connection accept error:", err)
			return err
		}
		phony.Block(tun, func() {
			if _, err := tun._wrap(conn.(*yggdrasil.Conn)); err != nil {
				// Something went wrong when storing the connection, typically that
				// something already exists for this address or subnet
				tun.log.Debugln("TUN/TAP handler wrap:", err)
			}
		})
	}
}

func (tun *TunAdapter) _wrap(conn *yggdrasil.Conn) (c *tunConn, err error) {
	// Prepare a session wrapper for the given connection
	s := tunConn{
		tun:  tun,
		conn: conn,
		stop: make(chan struct{}),
	}
	c = &s
	// Get the remote address and subnet of the other side
	remoteNodeID := conn.RemoteAddr().(*crypto.NodeID)
	s.addr = *address.AddrForNodeID(remoteNodeID)
	s.snet = *address.SubnetForNodeID(remoteNodeID)
	// Work out if this is already a destination we already know about
	atc, aok := tun.addrToConn[s.addr]
	stc, sok := tun.subnetToConn[s.snet]
	// If we know about a connection for this destination already then assume it
	// is no longer valid and close it
	if aok {
		atc._close_from_tun()
		err = errors.New("replaced connection for address")
	} else if sok {
		stc._close_from_tun()
		err = errors.New("replaced connection for subnet")
	}
	// Save the session wrapper so that we can look it up quickly next time
	// we receive a packet through the interface for this address
	tun.addrToConn[s.addr] = &s
	tun.subnetToConn[s.snet] = &s
	// Set the read callback and start the timeout
	conn.SetReadCallback(func(bs []byte) {
		s.Act(conn, func() {
			s._read(bs)
		})
	})
	s.Act(nil, s.stillAlive)
	// Return
	return c, err
}
