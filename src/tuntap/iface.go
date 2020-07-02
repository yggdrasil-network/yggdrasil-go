package tuntap

import (
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"

	"github.com/Arceliar/phony"
)

const TUN_OFFSET_BYTES = 4

type tunWriter struct {
	phony.Inbox
	tun *TunAdapter
	buf [TUN_OFFSET_BYTES + 65536]byte
}

func (w *tunWriter) writeFrom(from phony.Actor, b []byte) {
	w.Act(from, func() {
		w._write(b)
	})
}

// write is pretty loose with the memory safety rules, e.g. it assumes it can
// read w.tun.iface.IsTap() safely
func (w *tunWriter) _write(b []byte) {
	var written int
	var err error
	n := len(b)
	if n == 0 {
		return
	}
	temp := append(w.buf[:TUN_OFFSET_BYTES], b...)
	written, err = w.tun.iface.Write(temp, TUN_OFFSET_BYTES)
	if err != nil {
		w.tun.Act(w, func() {
			if !w.tun.isOpen {
				w.tun.log.Errorln("TUN iface write error:", err)
			}
		})
	}
	if written != n+TUN_OFFSET_BYTES {
		// FIXME some platforms return the wrong number of bytes written, causing error spam
		//w.tun.log.Errorln("TUN iface write mismatch:", written, "bytes written vs", n+TUN_OFFSET_BYTES, "bytes given")
	}
}

type tunReader struct {
	phony.Inbox
	tun *TunAdapter
	buf [TUN_OFFSET_BYTES + 65536]byte
}

func (r *tunReader) _read() {
	// Get a slice to store the packet in
	// Wait for a packet to be delivered to us through the TUN adapter
	n, err := r.tun.iface.Read(r.buf[:], TUN_OFFSET_BYTES)
	if n <= TUN_OFFSET_BYTES || err != nil {
		r.tun.log.Errorln("Error reading TUN:", err)
		ferr := r.tun.iface.Flush()
		if ferr != nil {
			r.tun.log.Errorln("Unable to flush packets:", ferr)
		}
	} else {
		bs := make([]byte, n, n+crypto.BoxOverhead) // extra capacity for later...
		copy(bs, r.buf[TUN_OFFSET_BYTES:n+TUN_OFFSET_BYTES])
		r.tun.handlePacketFrom(r, bs, err)
	}
	if err == nil {
		// Now read again
		r.Act(nil, r._read)
	}
}

func (tun *TunAdapter) handlePacketFrom(from phony.Actor, packet []byte, err error) {
	tun.Act(from, func() {
		tun._handlePacket(packet, err)
	})
}

// does the work of reading a packet and sending it to the correct tunConn
func (tun *TunAdapter) _handlePacket(recvd []byte, err error) {
	if err != nil {
		tun.log.Errorln("TUN iface read error:", err)
		return
	}
	// Offset the buffer from now on so that we can ignore ethernet frames if
	// they are present
	bs := recvd[:]
	// Check if the packet is long enough to detect if it's an ICMP packet or not
	if len(bs) < 7 {
		tun.log.Traceln("TUN iface read undersized unknown packet, length:", len(bs))
		return
	}
	// From the IP header, work out what our source and destination addresses
	// and node IDs are. We will need these in order to work out where to send
	// the packet
	var dstAddr address.Address
	var dstSnet address.Subnet
	var addrlen int
	n := len(bs)
	// Check the IP protocol - if it doesn't match then we drop the packet and
	// do nothing with it
	if bs[0]&0xf0 == 0x60 {
		// Check if we have a fully-sized IPv6 header
		if len(bs) < 40 {
			tun.log.Traceln("TUN iface read undersized ipv6 packet, length:", len(bs))
			return
		}
		// Check the packet size
		if n-tun_IPv6_HEADER_LENGTH != 256*int(bs[4])+int(bs[5]) {
			return
		}
		// IPv6 address
		addrlen = 16
		copy(dstAddr[:addrlen], bs[24:])
		copy(dstSnet[:addrlen/2], bs[24:])
	} else if bs[0]&0xf0 == 0x40 {
		// Check if we have a fully-sized IPv4 header
		if len(bs) < 20 {
			tun.log.Traceln("TUN iface read undersized ipv4 packet, length:", len(bs))
			return
		}
		// Check the packet size
		if n != 256*int(bs[2])+int(bs[3]) {
			return
		}
		// IPv4 address
		addrlen = 4
		copy(dstAddr[:addrlen], bs[16:])
	} else {
		// Unknown address length or protocol, so drop the packet and ignore it
		tun.log.Traceln("Unknown packet type, dropping")
		return
	}
	if tun.ckr.isEnabled() {
		if addrlen != 16 || (!dstAddr.IsValid() && !dstSnet.IsValid()) {
			if key, err := tun.ckr.getPublicKeyForAddress(dstAddr, addrlen); err == nil {
				// A public key was found, get the node ID for the search
				dstNodeID := crypto.GetNodeID(&key)
				dstAddr = *address.AddrForNodeID(dstNodeID)
				dstSnet = *address.SubnetForNodeID(dstNodeID)
				addrlen = 16
			}
		}
	}
	if addrlen != 16 || (!dstAddr.IsValid() && !dstSnet.IsValid()) {
		// Couldn't find this node's ygg IP
		return
	}

	var boxPubKey *crypto.BoxPubKey
	if key, ok := tun.addrToBoxPubKey[dstAddr]; ok {
		boxPubKey = key
	} else if key, ok := tun.subnetToBoxPubKey[dstSnet]; ok {
		boxPubKey = key
	} else {
		var dstNodeID, dstNodeMask *crypto.NodeID
		if dstAddr[0] == 0x02 {
			dstNodeID, dstNodeMask = dstAddr.GetNodeIDandMask()
		} else if dstAddr[0] == 0x03 {
			dstNodeID, dstNodeMask = dstSnet.GetNodeIDandMask()
		}
		if dstNodeID == nil || dstNodeMask == nil {
			tun.log.Errorln("Didn't find node ID/mask")
			return
		}

		_, boxPubKey, err = tun.core.Resolve(dstNodeID, dstNodeMask)
		if err != nil {
			tun.log.Errorln("tun.core.Resolve:", err)
			return
		}
		tun.addrToBoxPubKey[dstAddr] = boxPubKey
		tun.subnetToBoxPubKey[dstSnet] = boxPubKey
	}

	if boxPubKey == nil {
		tun.log.Errorln("No destination public key found for this packet")
		return
	}

	n, err = tun.packetConn.WriteTo(recvd, boxPubKey)
	if err != nil {
		tun.log.Errorln("tun.packetConn.WriteTo:", err)
	}
	if n != len(recvd) {
		tun.log.Errorln("Expected to send", len(recvd), "bytes but sent", n, "bytes")
	}
}
