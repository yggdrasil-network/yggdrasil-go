package tun

// This manages the tun driver to send/recv packets to/from applications

// TODO: Connection timeouts (call Conn.Close() when we want to time out)
// TODO: Don't block in reader on writes that are pending searches

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/Arceliar/phony"
	wgtun "golang.zx2c4.com/wireguard/tun"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

type MTU uint16

type ReadWriteCloser interface {
	io.ReadWriteCloser
	Address() address.Address
	Subnet() address.Subnet
	MaxMTU() uint64
	SetMTU(uint64)
}

// TunAdapter represents a running TUN interface and extends the
// yggdrasil.Adapter type. In order to use the TUN adapter with Yggdrasil, you
// should pass this object to the yggdrasil.SetRouterAdapter() function before
// calling yggdrasil.Start().
type TunAdapter struct {
	rwc         ReadWriteCloser
	log         core.Logger
	addr        address.Address
	subnet      address.Subnet
	mtu         uint64
	iface       wgtun.Device
	phony.Inbox // Currently only used for _handlePacket from the reader, TODO: all the stuff that currently needs a mutex below
	isOpen      bool
	isEnabled   bool // Used by the writer to drop sessionTraffic if not enabled
	config      struct {
		fd   int32
		name InterfaceName
		mtu  InterfaceMTU
	}
}

// Gets the maximum supported MTU for the platform based on the defaults in
// config.GetDefaults().
func getSupportedMTU(mtu uint64) uint64 {
	if mtu < 1280 {
		return 1280
	}
	if mtu > MaximumMTU() {
		return MaximumMTU()
	}
	return mtu
}

func waitForTUNUp(ch <-chan wgtun.Event) bool {
	t := time.After(time.Second * 5)
	for {
		select {
		case ev := <-ch:
			if ev == wgtun.EventUp {
				return true
			}
		case <-t:
			return false
		}
	}
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
	return config.GetDefaults().DefaultIfName
}

// DefaultMTU gets the default TUN interface MTU for your platform. This can
// be as high as MaximumMTU(), depending on platform, but is never lower than 1280.
func DefaultMTU() uint64 {
	return config.GetDefaults().DefaultIfMTU
}

// MaximumMTU returns the maximum supported TUN interface MTU for your
// platform. This can be as high as 65535, depending on platform, but is never
// lower than 1280.
func MaximumMTU() uint64 {
	return config.GetDefaults().MaximumIfMTU
}

// Init initialises the TUN module. You must have acquired a Listener from
// the Yggdrasil core before this point and it must not be in use elsewhere.
func New(rwc ReadWriteCloser, log core.Logger, opts ...SetupOption) (*TunAdapter, error) {
	tun := &TunAdapter{
		rwc: rwc,
		log: log,
	}
	for _, opt := range opts {
		tun._applyOption(opt)
	}
	return tun, tun._start()
}

func (tun *TunAdapter) _start() error {
	if tun.isOpen {
		return errors.New("TUN module is already started")
	}
	tun.addr = tun.rwc.Address()
	tun.subnet = tun.rwc.Subnet()
	prefix := address.GetPrefix()
	var addr string
	if tun.addr.IsValid() {
		addr = fmt.Sprintf("%s/%d", net.IP(tun.addr[:]).String(), 8*len(prefix[:])-1)
	}
	if tun.config.name == "none" || tun.config.name == "dummy" {
		tun.log.Debugln("Not starting TUN as ifname is none or dummy")
		tun.isEnabled = false
		go tun.write()
		return nil
	}
	mtu := uint64(tun.config.mtu)
	if tun.rwc.MaxMTU() < mtu {
		mtu = tun.rwc.MaxMTU()
	}
	var err error
	if tun.config.fd > 0 {
		err = tun.setupFD(tun.config.fd, addr, mtu)
	} else {
		err = tun.setup(string(tun.config.name), addr, mtu)
	}
	if err != nil {
		return err
	}
	if tun.MTU() != mtu {
		tun.log.Warnf("Warning: Interface MTU %d automatically adjusted to %d (supported range is 1280-%d)", tun.config.mtu, tun.MTU(), MaximumMTU())
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
