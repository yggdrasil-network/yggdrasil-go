package tuntap

import (
	"bytes"
	"errors"
	"net"
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
		var srcAddr address.Address
		var dstAddr address.Address
		var addrlen int
		// Check whether the packet is IPv4, IPv6 or neither
		if b[0]&0xf0 == 0x60 {
			// IPv6 packet found
			if len(b) < 40 {
				// Packet was too short
				util.PutBytes(b)
				continue
			}
			// Extract the IPv6 addresses
			copy(srcAddr[:16], b[8:24])
			copy(dstAddr[:16], b[24:40])
			addrlen = 16
		} else if b[0]&0xf0 == 0x40 {
			// IPv4 packet found
			if len(b) < 20 {
				// Packet was too short
				util.PutBytes(b)
				continue
			}
			// Extract the IPv4 addresses
			copy(srcAddr[:4], b[12:16])
			copy(dstAddr[:4], b[16:20])
			addrlen = 4
		} else {
			// Neither IPv4 nor IPv6
			return errors.New("Invalid address family")
		}
		// Check the crypto-key routing rules next
		if tun.ckr.isEnabled() {
			if !tun.ckr.isValidLocalAddress(dstAddr, addrlen) {
				util.PutBytes(b)
				continue
			}
			if srcAddr[0] != 0x02 && srcAddr[0] != 0x03 {
				// TODO: is this check useful? this doesn't actually guarantee that the
				// packet came from the configured public key for that remote, just that
				// it came from *a* configured remote. at this stage we have no ability
				// to know which Conn or public key was involved
				if _, err := tun.ckr.getPublicKeyForAddress(srcAddr, addrlen); err != nil {
					util.PutBytes(b)
					continue
				}
			}
		} else {
			if addrlen != 16 {
				util.PutBytes(b)
				continue
			}
			if !bytes.Equal(tun.addr[:16], dstAddr[:16]) && !bytes.Equal(tun.subnet[:8], dstAddr[:8]) {
				util.PutBytes(b)
				continue
			}
		}
		if tun.iface.IsTAP() {
			sendndp := func(dstAddr address.Address) {
				neigh, known := tun.icmpv6.getNeighbor(dstAddr)
				known = known && (time.Since(neigh.lastsolicitation).Seconds() < 30)
				if !known {
					tun.icmpv6.Solicit(dstAddr)
				}
			}
			peermac := net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
			var peerknown bool
			if b[0]&0xf0 == 0x40 {
				dstAddr = tun.addr
			} else if b[0]&0xf0 == 0x60 {
				if !bytes.Equal(tun.addr[:16], dstAddr[:16]) && !bytes.Equal(tun.subnet[:8], dstAddr[:8]) {
					dstAddr = tun.addr
				}
			}
			if neighbor, ok := tun.icmpv6.getNeighbor(dstAddr); ok && neighbor.learned {
				// If we've learned the MAC of a 300::/7 address, for example, or a CKR
				// address, use the MAC address of that
				peermac = neighbor.mac
				peerknown = true
			} else if neighbor, ok := tun.icmpv6.getNeighbor(tun.addr); ok && neighbor.learned {
				// Otherwise send directly to the MAC address of the host if that's
				// known instead
				peermac = neighbor.mac
				peerknown = true
			} else {
				// Nothing has been discovered, try to discover the destination
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
			} else {
				tun.log.Errorln("TUN/TAP iface write error: no peer MAC known for", net.IP(dstAddr[:]).String(), "- dropping packet")
			}
		} else {
			w, err = tun.iface.Write(b[:n])
			util.PutBytes(b)
		}
		if err != nil {
			if !tun.isOpen {
				return err
			}
			tun.log.Errorln("TUN/TAP iface write error:", err)
			continue
		}
		if w != n {
			tun.log.Errorln("TUN/TAP iface write mismatch:", w, "bytes written vs", n, "bytes given")
			continue
		}
	}
}

// Run in a separate goroutine by the reader
// Does all of the per-packet ICMP checks, passes packets to the right Conn worker
func (tun *TunAdapter) readerPacketHandler(ch chan []byte) {
	for recvd := range ch {
		// If it's a TAP adapter, update the buffer slice so that we no longer
		// include the ethernet headers
		offset := 0
		if tun.iface.IsTAP() {
			// Set our offset to beyond the ethernet headers
			offset = tun_ETHER_HEADER_LENGTH
			// Check first of all that we can go beyond the ethernet headers
			if len(recvd) <= offset {
				continue
			}
		}
		// Offset the buffer from now on so that we can ignore ethernet frames if
		// they are present
		bs := recvd[offset:]
		// If we detect an ICMP packet then hand it to the ICMPv6 module
		if bs[6] == 58 {
			// Found an ICMPv6 packet - we need to make sure to give ICMPv6 the full
			// Ethernet frame rather than just the IPv6 packet as this is needed for
			// NDP to work correctly
			if err := tun.icmpv6.ParsePacket(recvd); err == nil {
				// We acted on the packet in the ICMPv6 module so don't forward or do
				// anything else with it
				continue
			}
		}
		if offset != 0 {
			// Shift forward to avoid leaking bytes off the front of the slice when we eventually store it
			bs = append(recvd[:0], bs...)
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
		n := len(bs)
		// Check the IP protocol - if it doesn't match then we drop the packet and
		// do nothing with it
		if bs[0]&0xf0 == 0x60 {
			// Check if we have a fully-sized IPv6 header
			if len(bs) < 40 {
				continue
			}
			// Check the packet size
			if n-tun_IPv6_HEADER_LENGTH != 256*int(bs[4])+int(bs[5]) {
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
			if n != 256*int(bs[2])+int(bs[3]) {
				continue
			}
			// IPv4 address
			addrlen = 4
			copy(srcAddr[:addrlen], bs[12:])
			copy(dstAddr[:addrlen], bs[16:])
		} else {
			// Unknown address length or protocol, so drop the packet and ignore it
			tun.log.Traceln("Unknown packet type, dropping")
			continue
		}
		if tun.ckr.isEnabled() {
			if !tun.ckr.isValidLocalAddress(srcAddr, addrlen) {
				continue
			}
			if !dstAddr.IsValid() && !dstSnet.IsValid() {
				if key, err := tun.ckr.getPublicKeyForAddress(dstAddr, addrlen); err == nil {
					// A public key was found, get the node ID for the search
					dstNodeID = crypto.GetNodeID(&key)
					// Do a quick check to ensure that the node ID refers to a vaild
					// Yggdrasil address or subnet - this might be superfluous
					addr := *address.AddrForNodeID(dstNodeID)
					copy(dstAddr[:], addr[:])
					copy(dstSnet[:], addr[:])
					// Are we certain we looked up a valid node?
					if !dstAddr.IsValid() && !dstSnet.IsValid() {
						continue
					}
				} else {
					// No public key was found in the CKR table so we've exhausted our options
					continue
				}
			}
		} else {
			if addrlen != 16 {
				continue
			}
			if !dstAddr.IsValid() && !dstSnet.IsValid() {
				continue
			}
		}
		// Do we have an active connection for this node address?
		tun.mutex.RLock()
		session, isIn := tun.addrToConn[dstAddr]
		if !isIn || session == nil {
			session, isIn = tun.subnetToConn[dstSnet]
			if !isIn || session == nil {
				// Neither an address nor a subnet mapping matched, therefore populate
				// the node ID and mask to commence a search
				if dstAddr.IsValid() {
					dstNodeID, dstNodeIDMask = dstAddr.GetNodeIDandMask()
				} else {
					dstNodeID, dstNodeIDMask = dstSnet.GetNodeIDandMask()
				}
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
			go func() {
				// FIXME just spitting out a goroutine to do this is kind of ugly and means we drop packets until the dial finishes
				tun.mutex.Lock()
				_, known := tun.dials[*dstNodeID]
				tun.dials[*dstNodeID] = append(tun.dials[*dstNodeID], bs)
				for len(tun.dials[*dstNodeID]) > 32 {
					util.PutBytes(tun.dials[*dstNodeID][0])
					tun.dials[*dstNodeID] = tun.dials[*dstNodeID][1:]
				}
				tun.mutex.Unlock()
				if known {
					return
				}
				var tc *tunConn
				if conn, err := tun.dialer.DialByNodeIDandMask(dstNodeID, dstNodeIDMask); err == nil {
					// We've been given a connection so prepare the session wrapper
					if tc, err = tun.wrap(conn); err != nil {
						// Something went wrong when storing the connection, typically that
						// something already exists for this address or subnet
						tun.log.Debugln("TUN/TAP iface wrap:", err)
					}
				}
				tun.mutex.Lock()
				packets := tun.dials[*dstNodeID]
				delete(tun.dials, *dstNodeID)
				tun.mutex.Unlock()
				if tc != nil {
					for _, packet := range packets {
						p := packet // Possibly required because of how range
						tc.send <- p
					}
				}
			}()
			// While the dial is going on we can't do much else
			// continuing this iteration - skip to the next one
			continue
		}
		// If we have a connection now, try writing to it
		if isIn && session != nil {
			session.send <- bs
		}
	}
}

func (tun *TunAdapter) reader() error {
	toWorker := make(chan []byte, 32)
	defer close(toWorker)
	go tun.readerPacketHandler(toWorker)
	for {
		// Get a slice to store the packet in
		recvd := util.ResizeBytes(util.GetBytes(), 65535+tun_ETHER_HEADER_LENGTH)
		// Wait for a packet to be delivered to us through the TUN/TAP adapter
		n, err := tun.iface.Read(recvd)
		if err != nil {
			if !tun.isOpen {
				return err
			}
			panic(err)
		}
		if n == 0 {
			util.PutBytes(recvd)
			continue
		}
		// Send the packet to the worker
		toWorker <- recvd[:n]
	}
}
