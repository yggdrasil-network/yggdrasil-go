package tuntap

// This manages the tun driver to send/recv packets to/from applications

import (
	"encoding/hex"
	"errors"
	"net"
	"sync"
	"time"

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
	config      *config.NodeState
	log         *log.Logger
	reconfigure chan chan error
	conns       map[crypto.NodeID]*yggdrasil.Conn
	listener    *yggdrasil.Listener
	dialer      *yggdrasil.Dialer
	addr        address.Address
	subnet      address.Subnet
	icmpv6      ICMPv6
	mtu         int
	iface       *water.Interface
	mutex       sync.RWMutex // Protects the below
	isOpen      bool
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
	tun.conns = make(map[crypto.NodeID]*yggdrasil.Conn)
}

// Start the setup process for the TUN/TAP adapter. If successful, starts the
// read/write goroutines to handle packets on that interface.
func (tun *TunAdapter) Start() error {
	tun.config.Mutex.Lock()
	defer tun.config.Mutex.Unlock()
	if tun.config == nil || tun.listener == nil || tun.dialer == nil {
		return errors.New("No configuration available to TUN/TAP")
	}
	var boxPub crypto.BoxPubKey
	boxPubHex, err := hex.DecodeString(tun.config.Current.EncryptionPublicKey)
	if err != nil {
		return err
	}
	copy(boxPub[:], boxPubHex)
	nodeID := crypto.GetNodeID(&boxPub)
	tun.addr = *address.AddrForNodeID(nodeID)
	tun.subnet = *address.SubnetForNodeID(nodeID)
	tun.mtu = tun.config.Current.IfMTU
	ifname := tun.config.Current.IfName
	iftapmode := tun.config.Current.IfTAPMode
	if ifname != "none" {
		if err := tun.setup(ifname, iftapmode, net.IP(tun.addr[:]).String(), tun.mtu); err != nil {
			return err
		}
	}
	if ifname == "none" || ifname == "dummy" {
		tun.log.Debugln("Not starting TUN/TAP as ifname is none or dummy")
		return nil
	}
	tun.mutex.Lock()
	tun.isOpen = true
	tun.mutex.Unlock()
	if iftapmode {
		go func() {
			for {
				if _, ok := tun.icmpv6.peermacs[tun.addr]; ok {
					break
				}
				request, err := tun.icmpv6.CreateNDPL2(tun.addr)
				if err != nil {
					panic(err)
				}
				if _, err := tun.iface.Write(request); err != nil {
					panic(err)
				}
				time.Sleep(time.Second)
			}
		}()
	}
	tun.icmpv6.Init(tun)
	go func() {
		for {
			e := <-tun.reconfigure
			e <- nil
		}
	}()
	go tun.handler()
	go tun.ifaceReader()
	return nil
}

func (tun *TunAdapter) handler() error {
	for {
		// Accept the incoming connection
		conn, err := tun.listener.Accept()
		if err != nil {
			tun.log.Errorln("TUN/TAP error accepting connection:", err)
			return err
		}
		tun.log.Println("Accepted connection from", conn.RemoteAddr())
		go tun.connReader(conn)
	}
}

func (tun *TunAdapter) connReader(conn *yggdrasil.Conn) error {
	tun.conns[conn.RemoteAddr()] = conn
	b := make([]byte, 65535)
	for {
		n, err := conn.Read(b)
		if err != nil {
			tun.log.Errorln("TUN/TAP read error:", err)
			return err
		}
		if n == 0 {
			continue
		}
		w, err := tun.iface.Write(b[:n])
		if err != nil {
			tun.log.Errorln("TUN/TAP failed to write to interface:", err)
			continue
		}
		if w != n {
			tun.log.Errorln("TUN/TAP wrote", w, "instead of", n, "which is bad")
			continue
		}
	}
}

func (tun *TunAdapter) ifaceReader() error {
	bs := make([]byte, 65535)
	for {
		n, err := tun.iface.Read(bs)
		if err != nil {
			tun.log.Errorln("TUN/TAP iface read error:", err)
		}
		// Look up if the dstination address is somewhere we already have an
		// open connection to
		var srcAddr address.Address
		var dstAddr address.Address
		var dstNodeID *crypto.NodeID
		var dstNodeIDMask *crypto.NodeID
		var dstSnet address.Subnet
		var addrlen int
		if bs[0]&0xf0 == 0x60 {
			// Check if we have a fully-sized header
			if len(bs) < 40 {
				panic("Tried to send a packet shorter than an IPv6 header...")
			}
			// IPv6 address
			addrlen = 16
			copy(srcAddr[:addrlen], bs[8:])
			copy(dstAddr[:addrlen], bs[24:])
			copy(dstSnet[:addrlen/2], bs[24:])
		} else if bs[0]&0xf0 == 0x40 {
			// Check if we have a fully-sized header
			if len(bs) < 20 {
				panic("Tried to send a packet shorter than an IPv4 header...")
			}
			// IPv4 address
			addrlen = 4
			copy(srcAddr[:addrlen], bs[12:])
			copy(dstAddr[:addrlen], bs[16:])
		} else {
			// Unknown address length
			continue
		}
		dstNodeID, dstNodeIDMask = dstAddr.GetNodeIDandMask()
		// Do we have an active connection for this node ID?
		if conn, isIn := tun.conns[*dstNodeID]; isIn {
			tun.log.Println("Got", &conn)
			w, err := conn.Write(bs)
			if err != nil {
				tun.log.Println("Unable to write to remote:", err)
				continue
			}
			if w != n {
				continue
			}
		} else {
			tun.log.Println("Opening connection for", *dstNodeID)
			if conn, err := tun.dialer.DialByNodeIDandMask(dstNodeID, dstNodeIDMask); err == nil {
				tun.conns[*dstNodeID] = &conn
				go tun.connReader(&conn)
			} else {
				tun.log.Println("Error dialing:", err)
			}
		}

		/*if !r.cryptokey.isValidSource(srcAddr, addrlen) {
			// The packet had a src address that doesn't belong to us or our
			// configured crypto-key routing src subnets
			return
		}
		if !dstAddr.IsValid() && !dstSnet.IsValid() {
			// The addresses didn't match valid Yggdrasil node addresses so let's see
			// whether it matches a crypto-key routing range instead
			if key, err := r.cryptokey.getPublicKeyForAddress(dstAddr, addrlen); err == nil {
				// A public key was found, get the node ID for the search
				dstPubKey = &key
				dstNodeID = crypto.GetNodeID(dstPubKey)
				// Do a quick check to ensure that the node ID refers to a vaild Yggdrasil
				// address or subnet - this might be superfluous
				addr := *address.AddrForNodeID(dstNodeID)
				copy(dstAddr[:], addr[:])
				copy(dstSnet[:], addr[:])
				if !dstAddr.IsValid() && !dstSnet.IsValid() {
					return
				}
			} else {
				// No public key was found in the CKR table so we've exhausted our options
				return
			}
		}*/

	}
}

// Writes a packet to the TUN/TAP adapter. If the adapter is running in TAP
// mode then additional ethernet encapsulation is added for the benefit of the
// host operating system.
/*
func (tun *TunAdapter) write() error {
	for {
		select {
		case reject := <-tun.Reject:
		switch reject.Reason {
		case yggdrasil.PacketTooBig:
			if mtu, ok := reject.Detail.(int); ok {
				// Create the Packet Too Big response
				ptb := &icmp.PacketTooBig{
					MTU:  int(mtu),
					Data: reject.Packet,
				}

				// Create the ICMPv6 response from it
				icmpv6Buf, err := CreateICMPv6(
					reject.Packet[8:24], reject.Packet[24:40],
					ipv6.ICMPTypePacketTooBig, 0, ptb)

				// Send the ICMPv6 response back to the TUN/TAP adapter
				if err == nil {
					tun.iface.Write(icmpv6Buf)
				}
			}
			fallthrough
		default:
			continue
		}
		case data := <-tun.Recv:
			if tun.iface == nil {
				continue
			}
			if tun.iface.IsTAP() {
				var dstAddr address.Address
				if data[0]&0xf0 == 0x60 {
					if len(data) < 40 {
						//panic("Tried to send a packet shorter than an IPv6 header...")
						util.PutBytes(data)
						continue
					}
					copy(dstAddr[:16], data[24:])
				} else if data[0]&0xf0 == 0x40 {
					if len(data) < 20 {
						//panic("Tried to send a packet shorter than an IPv4 header...")
						util.PutBytes(data)
						continue
					}
					copy(dstAddr[:4], data[16:])
				} else {
					return errors.New("Invalid address family")
				}
				sendndp := func(dstAddr address.Address) {
					neigh, known := tun.icmpv6.peermacs[dstAddr]
					known = known && (time.Since(neigh.lastsolicitation).Seconds() < 30)
					if !known {
						request, err := tun.icmpv6.CreateNDPL2(dstAddr)
						if err != nil {
							panic(err)
						}
						if _, err := tun.iface.Write(request); err != nil {
							panic(err)
						}
						tun.icmpv6.peermacs[dstAddr] = neighbor{
							lastsolicitation: time.Now(),
						}
					}
				}
				var peermac macAddress
				var peerknown bool
				if data[0]&0xf0 == 0x40 {
					dstAddr = tun.addr
				} else if data[0]&0xf0 == 0x60 {
					if !bytes.Equal(tun.addr[:16], dstAddr[:16]) && !bytes.Equal(tun.subnet[:8], dstAddr[:8]) {
						dstAddr = tun.addr
					}
				}
				if neighbor, ok := tun.icmpv6.peermacs[dstAddr]; ok && neighbor.learned {
					peermac = neighbor.mac
					peerknown = true
				} else if neighbor, ok := tun.icmpv6.peermacs[tun.addr]; ok && neighbor.learned {
					peermac = neighbor.mac
					peerknown = true
					sendndp(dstAddr)
				} else {
					sendndp(tun.addr)
				}
				if peerknown {
					var proto ethernet.Ethertype
					switch {
					case data[0]&0xf0 == 0x60:
						proto = ethernet.IPv6
					case data[0]&0xf0 == 0x40:
						proto = ethernet.IPv4
					}
					var frame ethernet.Frame
					frame.Prepare(
						peermac[:6],          // Destination MAC address
						tun.icmpv6.mymac[:6], // Source MAC address
						ethernet.NotTagged,   // VLAN tagging
						proto,                // Ethertype
						len(data))            // Payload length
					copy(frame[tun_ETHER_HEADER_LENGTH:], data[:])
					if _, err := tun.iface.Write(frame); err != nil {
						tun.mutex.RLock()
						open := tun.isOpen
						tun.mutex.RUnlock()
						if !open {
							return nil
						} else {
							panic(err)
						}
					}
				}
			} else {
				if _, err := tun.iface.Write(data); err != nil {
					tun.mutex.RLock()
					open := tun.isOpen
					tun.mutex.RUnlock()
					if !open {
						return nil
					} else {
						panic(err)
					}
				}
			}
			util.PutBytes(data)
		}
	}
}

// Reads any packets that are waiting on the TUN/TAP adapter. If the adapter
// is running in TAP mode then the ethernet headers will automatically be
// processed and stripped if necessary. If an ICMPv6 packet is found, then
// the relevant helper functions in icmpv6.go are called.
func (tun *TunAdapter) read() error {
	mtu := tun.mtu
	if tun.iface.IsTAP() {
		mtu += tun_ETHER_HEADER_LENGTH
	}
	buf := make([]byte, mtu)
	for {
		n, err := tun.iface.Read(buf)
		if err != nil {
			tun.mutex.RLock()
			open := tun.isOpen
			tun.mutex.RUnlock()
			if !open {
				return nil
			} else {
				return err
			}
		}
		o := 0
		if tun.iface.IsTAP() {
			o = tun_ETHER_HEADER_LENGTH
		}
		switch {
		case buf[o]&0xf0 == 0x60 && n == 256*int(buf[o+4])+int(buf[o+5])+tun_IPv6_HEADER_LENGTH+o:
		case buf[o]&0xf0 == 0x40 && n == 256*int(buf[o+2])+int(buf[o+3])+o:
		default:
			continue
		}
		if buf[o+6] == 58 {
			if tun.iface.IsTAP() {
				// Found an ICMPv6 packet
				b := make([]byte, n)
				copy(b, buf)
				go tun.icmpv6.ParsePacket(b)
			}
		}
		packet := append(util.GetBytes(), buf[o:n]...)
		tun.Send <- packet
	}
}

// Closes the TUN/TAP adapter. This is only usually called when the Yggdrasil
// process stops. Typically this operation will happen quickly, but on macOS
// it can block until a read operation is completed.
func (tun *TunAdapter) Close() error {
	tun.mutex.Lock()
	tun.isOpen = false
	tun.mutex.Unlock()
	if tun.iface == nil {
		return nil
	}
	return tun.iface.Close()
}
*/
