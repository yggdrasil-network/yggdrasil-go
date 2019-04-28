package tuntap

import (
	"bytes"
	"errors"
	"time"

	"github.com/songgao/packets/ethernet"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

func (tun *TunAdapter) writer() error {
	var w int
	var err error
	for {
		b := <-tun.send
		n := len(b)
		if n == 0 {
			continue
		}
		if tun.iface.IsTAP() {
			var dstAddr address.Address
			if b[0]&0xf0 == 0x60 {
				if len(b) < 40 {
					//panic("Tried to send a packet shorter than an IPv6 header...")
					util.PutBytes(b)
					continue
				}
				copy(dstAddr[:16], b[24:])
			} else if b[0]&0xf0 == 0x40 {
				if len(b) < 20 {
					//panic("Tried to send a packet shorter than an IPv4 header...")
					util.PutBytes(b)
					continue
				}
				copy(dstAddr[:4], b[16:])
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
			if b[0]&0xf0 == 0x40 {
				dstAddr = tun.addr
			} else if b[0]&0xf0 == 0x60 {
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
				case b[0]&0xf0 == 0x60:
					proto = ethernet.IPv6
				case b[0]&0xf0 == 0x40:
					proto = ethernet.IPv4
				}
				var frame ethernet.Frame
				frame.Prepare(
					peermac[:6],          // Destination MAC address
					tun.icmpv6.mymac[:6], // Source MAC address
					ethernet.NotTagged,   // VLAN tagging
					proto,                // Ethertype
					len(b))               // Payload length
				copy(frame[tun_ETHER_HEADER_LENGTH:], b[:n])
				n += tun_ETHER_HEADER_LENGTH
				w, err = tun.iface.Write(frame[:n])
			}
		} else {
			w, err = tun.iface.Write(b[:n])
		}
		if err != nil {
			tun.log.Errorln("TUN/TAP iface write error:", err)
			continue
		}
		if w != n {
			tun.log.Errorln("TUN/TAP iface write mismatch:", w, "bytes written vs", n, "bytes given")
			continue
		}
	}
}

func (tun *TunAdapter) reader() error {
	bs := make([]byte, 65535)
	for {
		// Wait for a packet to be delivered to us through the TUN/TAP adapter
		n, err := tun.iface.Read(bs)
		if err != nil {
			panic(err)
		}
		if n == 0 {
			continue
		}
		// If it's a TAP adapter, update the buffer slice so that we no longer
		// include the ethernet headers
		offset := 0
		if tun.iface.IsTAP() {
			// Set our offset to beyond the ethernet headers
			offset = tun_ETHER_HEADER_LENGTH
			// If we detect an ICMP packet then hand it to the ICMPv6 module
			if bs[offset+6] == 58 {
				// Found an ICMPv6 packet
				b := make([]byte, n)
				copy(b, bs)
				go tun.icmpv6.ParsePacket(b)
			}
			// Then offset the buffer so that we can now just treat it as an IP
			// packet from now on
			bs = bs[offset:]
		}
		// From the IP header, work out what our source and destination addresses
		// and node IDs are. We will need these in order to work out where to send
		// the packet
		var srcAddr address.Address
		var dstAddr address.Address
		var dstNodeID *crypto.NodeID
		var dstNodeIDMask *crypto.NodeID
		var dstSnet address.Subnet
		var addrlen int
		// Check the IP protocol - if it doesn't match then we drop the packet and
		// do nothing with it
		if bs[0]&0xf0 == 0x60 {
			// Check if we have a fully-sized IPv6 header
			if len(bs) < 40 {
				continue
			}
			// Check the packet size
			if n != 256*int(bs[4])+int(bs[5])+offset+tun_IPv6_HEADER_LENGTH {
				continue
			}
			// IPv6 address
			addrlen = 16
			copy(srcAddr[:addrlen], bs[8:])
			copy(dstAddr[:addrlen], bs[24:])
			copy(dstSnet[:addrlen/2], bs[24:])
		} else if bs[0]&0xf0 == 0x40 {
			// Check if we have a fully-sized IPv4 header
			if len(bs) < 20 {
				continue
			}
			// Check the packet size
			if n != 256*int(bs[2])+int(bs[3])+offset {
				continue
			}
			// IPv4 address
			addrlen = 4
			copy(srcAddr[:addrlen], bs[12:])
			copy(dstAddr[:addrlen], bs[16:])
		} else {
			// Unknown address length or protocol, so drop the packet and ignore it
			continue
		}
		if !dstAddr.IsValid() && !dstSnet.IsValid() {
			// For now don't deal with any non-Yggdrasil ranges
			continue
		}
		// Do we have an active connection for this node address?
		tun.mutex.RLock()
		session, isIn := tun.addrToConn[dstAddr]
		if !isIn || session == nil {
			session, isIn = tun.subnetToConn[dstSnet]
			if !isIn || session == nil {
				// Neither an address nor a subnet mapping matched, therefore populate
				// the node ID and mask to commence a search
				dstNodeID, dstNodeIDMask = dstAddr.GetNodeIDandMask()
			}
		}
		tun.mutex.RUnlock()
		// If we don't have a connection then we should open one
		if !isIn || session == nil {
			// Check we haven't been given empty node ID, really this shouldn't ever
			// happen but just to be sure...
			if dstNodeID == nil || dstNodeIDMask == nil {
				panic("Given empty dstNodeID and dstNodeIDMask - this shouldn't happen")
			}
			// Dial to the remote node
			if conn, err := tun.dialer.DialByNodeIDandMask(dstNodeID, dstNodeIDMask); err == nil {
				// We've been given a connection so prepare the session wrapper
				if s, err := tun.wrap(conn); err != nil {
					// Something went wrong when storing the connection, typically that
					// something already exists for this address or subnet
					tun.log.Debugln("TUN/TAP iface wrap:", err)
				} else {
					// Update our reference to the connection
					session, isIn = s, true
				}
			} else {
				// We weren't able to dial for some reason so there's no point in
				// continuing this iteration - skip to the next one
				continue
			}
		}
		// If we have a connection now, try writing to it
		if isIn && session != nil {
			select {
			case session.send <- bs[:n]:
			default:
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
