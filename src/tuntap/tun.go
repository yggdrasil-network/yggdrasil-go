package tuntap

// This manages the tun driver to send/recv packets to/from applications

// TODO: Crypto-key routing support
// TODO: Set MTU of session properly
// TODO: Reject packets that exceed session MTU with ICMPv6 for PMTU Discovery
// TODO: Connection timeouts (call Conn.Close() when we want to time out)
// TODO: Don't block in reader on writes that are pending searches

import (
	"errors"
	"fmt"
	"net"

	//"sync"

	"github.com/Arceliar/phony"
	"github.com/gologme/log"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
	"github.com/yggdrasil-network/yggdrasil-go/src/ipv6rwc"
)

type MTU uint16

// TunAdapter represents a running TUN interface and extends the
// yggdrasil.Adapter type. In order to use the TUN adapter with Yggdrasil, you
// should pass this object to the yggdrasil.SetRouterAdapter() function before
// calling yggdrasil.Start().
type TunAdapter struct {
	rwc         *ipv6rwc.ReadWriteCloser
	config      *config.NodeConfig
	log         *log.Logger
	addr        address.Address
	subnet      address.Subnet
	mtu         uint64
	iface       tun.Device
	phony.Inbox // Currently only used for _handlePacket from the reader, TODO: all the stuff that currently needs a mutex below
	//mutex        sync.RWMutex // Protects the below
	isOpen    bool
	isEnabled bool // Used by the writer to drop sessionTraffic if not enabled
}

// Gets the maximum supported MTU for the platform based on the defaults in
// defaults.GetDefaults().
func getSupportedMTU(mtu uint64) uint64 {
	if mtu < 1280 {
		return 1280
	}
	if mtu > MaximumMTU() {
		return MaximumMTU()
	}
	return mtu
}

// Name returns the name of the adapter, e.g. "tun0". On Windows, this may
// return a canonical adapter name instead.
func (tun *TunAdapter) Name() string {
	if name, err := tun.iface.Name(); err == nil {
		return name
	}
	return ""
}

// MTU gets the adapter's MTU. This can range between 1280 and 65535, although
// the maximum value is determined by your platform. The returned value will
// never exceed that of MaximumMTU().
func (tun *TunAdapter) MTU() uint64 {
	return getSupportedMTU(tun.mtu)
}

// DefaultName gets the default TUN interface name for your platform.
func DefaultName() string {
	return defaults.GetDefaults().DefaultIfName
}

// DefaultMTU gets the default TUN interface MTU for your platform. This can
// be as high as MaximumMTU(), depending on platform, but is never lower than 1280.
func DefaultMTU() uint64 {
	return defaults.GetDefaults().DefaultIfMTU
}

// MaximumMTU returns the maximum supported TUN interface MTU for your
// platform. This can be as high as 65535, depending on platform, but is never
// lower than 1280.
func MaximumMTU() uint64 {
	return defaults.GetDefaults().MaximumIfMTU
}

// Init initialises the TUN module. You must have acquired a Listener from
// the Yggdrasil core before this point and it must not be in use elsewhere.
func (tun *TunAdapter) Init(rwc *ipv6rwc.ReadWriteCloser, config *config.NodeConfig, log *log.Logger, options interface{}) error {
	tun.rwc = rwc
	tun.config = config
	tun.log = log
	return nil
}

// Start the setup process for the TUN adapter. If successful, starts the
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
		return errors.New("TUN module is already started")
	}
	if tun.config == nil {
		return errors.New("no configuration available to TUN")
	}
	tun.config.RLock()
	defer tun.config.RUnlock()
	tun.addr = tun.rwc.Address()
	tun.subnet = tun.rwc.Subnet()
	addr := fmt.Sprintf("%s/%d", net.IP(tun.addr[:]).String(), 8*len(address.GetPrefix())-1)
	if tun.config.IfName == "none" || tun.config.IfName == "dummy" {
		tun.log.Debugln("Not starting TUN as ifname is none or dummy")
		tun.isEnabled = false
		go tun.write()
		return nil
	}
	mtu := tun.config.IfMTU
	if tun.rwc.MaxMTU() < mtu {
		mtu = tun.rwc.MaxMTU()
	}
	if err := tun.setup(tun.config.IfName, addr, mtu); err != nil {
		return err
	}
	if tun.MTU() != mtu {
		tun.log.Warnf("Warning: Interface MTU %d automatically adjusted to %d (supported range is 1280-%d)", tun.config.IfMTU, tun.MTU(), MaximumMTU())
	}
	tun.rwc.SetMTU(tun.MTU())
	tun.isOpen = true
	tun.isEnabled = true
	go tun.read()
	go tun.write()
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

// Start the setup process for the TUN adapter. If successful, starts the
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
	// by TUN, e.g. readers/writers, sessions
	if tun.iface != nil {
		// Just in case we failed to start up the iface for some reason, this can apparently happen on Windows
		tun.iface.Close()
	}
	return nil
}
