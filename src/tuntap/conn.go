package tuntap

import (
	"bytes"
	"errors"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
)

const tunConnTimeout = 2 * time.Minute

type tunConn struct {
	tun   *TunAdapter
	conn  *yggdrasil.Conn
	addr  address.Address
	snet  address.Subnet
	send  chan []byte
	stop  chan struct{}
	alive chan struct{}
}

func (s *tunConn) close() {
	s.tun.mutex.Lock()
	defer s.tun.mutex.Unlock()
	s._close_nomutex()
}

func (s *tunConn) _close_nomutex() {
	s.conn.Close()
	delete(s.tun.addrToConn, s.addr)
	delete(s.tun.subnetToConn, s.snet)
	func() {
		defer func() { recover() }()
		close(s.stop) // Closes reader/writer goroutines
	}()
	func() {
		defer func() { recover() }()
		close(s.alive) // Closes timeout goroutine
	}()
}

func (s *tunConn) reader() (err error) {
	select {
	case _, ok := <-s.stop:
		if !ok {
			return errors.New("session was already closed")
		}
	default:
	}
	s.tun.log.Debugln("Starting conn reader for", s.conn.String())
	defer s.tun.log.Debugln("Stopping conn reader for", s.conn.String())
	for {
		select {
		case <-s.stop:
			return nil
		default:
		}
		var bs []byte
		if bs, err = s.conn.ReadNoCopy(); err != nil {
			if e, eok := err.(yggdrasil.ConnError); eok && !e.Temporary() {
				if e.Closed() {
					s.tun.log.Debugln(s.conn.String(), "TUN/TAP conn read debug:", err)
				} else {
					s.tun.log.Errorln(s.conn.String(), "TUN/TAP conn read error:", err)
				}
				return e
			}
		} else if len(bs) > 0 {
			ipv4 := len(bs) > 20 && bs[0]&0xf0 == 0x40
			ipv6 := len(bs) > 40 && bs[0]&0xf0 == 0x60
			isCGA := true
			// Check source addresses
			switch {
			case ipv6 && bs[8] == 0x02 && bytes.Equal(s.addr[:16], bs[8:24]): // source
			case ipv6 && bs[8] == 0x03 && bytes.Equal(s.snet[:8], bs[8:16]): // source
			default:
				isCGA = false
			}
			// Check destiantion addresses
			switch {
			case ipv6 && bs[24] == 0x02 && bytes.Equal(s.tun.addr[:16], bs[24:40]): // destination
			case ipv6 && bs[24] == 0x03 && bytes.Equal(s.tun.subnet[:8], bs[24:32]): // destination
			default:
				isCGA = false
			}
			// Decide how to handle the packet
			var skip bool
			switch {
			case isCGA: // Allowed
			case s.tun.ckr.isEnabled() && (ipv4 || ipv6):
				var srcAddr address.Address
				var dstAddr address.Address
				var addrlen int
				if ipv4 {
					copy(srcAddr[:], bs[12:16])
					copy(dstAddr[:], bs[16:20])
					addrlen = 4
				}
				if ipv6 {
					copy(srcAddr[:], bs[8:24])
					copy(dstAddr[:], bs[24:40])
					addrlen = 16
				}
				if !s.tun.ckr.isValidLocalAddress(dstAddr, addrlen) {
					// The destination address isn't in our CKR allowed range
					skip = true
				} else if key, err := s.tun.ckr.getPublicKeyForAddress(srcAddr, addrlen); err == nil {
					srcNodeID := crypto.GetNodeID(&key)
					if s.conn.RemoteAddr() == *srcNodeID {
						// This is the one allowed CKR case, where source and destination addresses are both good
					} else {
						// The CKR key associated with this address doesn't match the sender's NodeID
						skip = true
					}
				} else {
					// We have no CKR route for this source address
					skip = true
				}
			default:
				skip = true
			}
			if skip {
				util.PutBytes(bs)
				continue
			}
			s.tun.send <- bs
			s.stillAlive()
		} else {
			util.PutBytes(bs)
		}
	}
}

func (s *tunConn) writer() error {
	select {
	case _, ok := <-s.stop:
		if !ok {
			return errors.New("session was already closed")
		}
	default:
	}
	s.tun.log.Debugln("Starting conn writer for", s.conn.String())
	defer s.tun.log.Debugln("Stopping conn writer for", s.conn.String())
	for {
		select {
		case <-s.stop:
			return nil
		case bs, ok := <-s.send:
			if !ok {
				return errors.New("send closed")
			}
			v4 := len(bs) > 20 && bs[0]&0xf0 == 0x40
			v6 := len(bs) > 40 && bs[0]&0xf0 == 0x60
			isCGA := true
			// Check source addresses
			switch {
			case v6 && bs[8] == 0x02 && bytes.Equal(s.tun.addr[:16], bs[8:24]): // source
			case v6 && bs[8] == 0x03 && bytes.Equal(s.tun.subnet[:8], bs[8:16]): // source
			default:
				isCGA = false
			}
			// Check destiantion addresses
			switch {
			case v6 && bs[24] == 0x02 && bytes.Equal(s.addr[:16], bs[24:40]): // destination
			case v6 && bs[24] == 0x03 && bytes.Equal(s.snet[:8], bs[24:32]): // destination
			default:
				isCGA = false
			}
			// Decide how to handle the packet
			var skip bool
			switch {
			case isCGA: // Allowed
			case s.tun.ckr.isEnabled() && (v4 || v6):
				var srcAddr address.Address
				var dstAddr address.Address
				var addrlen int
				if v4 {
					copy(srcAddr[:], bs[12:16])
					copy(dstAddr[:], bs[16:20])
					addrlen = 4
				}
				if v6 {
					copy(srcAddr[:], bs[8:24])
					copy(dstAddr[:], bs[24:40])
					addrlen = 16
				}
				if !s.tun.ckr.isValidLocalAddress(srcAddr, addrlen) {
					// The source address isn't in our CKR allowed range
					skip = true
				} else if key, err := s.tun.ckr.getPublicKeyForAddress(dstAddr, addrlen); err == nil {
					dstNodeID := crypto.GetNodeID(&key)
					if s.conn.RemoteAddr() == *dstNodeID {
						// This is the one allowed CKR case, where source and destination addresses are both good
					} else {
						// The CKR key associated with this address doesn't match the sender's NodeID
						skip = true
					}
				} else {
					// We have no CKR route for this destination address... why do we have the packet in the first place?
					skip = true
				}
			default:
				skip = true
			}
			if skip {
				util.PutBytes(bs)
				continue
			}
			msg := yggdrasil.FlowKeyMessage{
				FlowKey: util.GetFlowKey(bs),
				Message: bs,
			}
			if err := s.conn.WriteNoCopy(msg); err != nil {
				if e, eok := err.(yggdrasil.ConnError); !eok {
					if e.Closed() {
						s.tun.log.Debugln(s.conn.String(), "TUN/TAP generic write debug:", err)
					} else {
						s.tun.log.Errorln(s.conn.String(), "TUN/TAP generic write error:", err)
					}
				} else if e.PacketTooBig() {
					// TODO: This currently isn't aware of IPv4 for CKR
					ptb := &icmp.PacketTooBig{
						MTU:  int(e.PacketMaximumSize()),
						Data: bs[:900],
					}
					if packet, err := CreateICMPv6(bs[8:24], bs[24:40], ipv6.ICMPTypePacketTooBig, 0, ptb); err == nil {
						s.tun.send <- packet
					}
				} else {
					if e.Closed() {
						s.tun.log.Debugln(s.conn.String(), "TUN/TAP conn write debug:", err)
					} else {
						s.tun.log.Errorln(s.conn.String(), "TUN/TAP conn write error:", err)
					}
				}
			} else {
				s.stillAlive()
			}
		}
	}
}

func (s *tunConn) stillAlive() {
	defer func() { recover() }()
	select {
	case s.alive <- struct{}{}:
	default:
	}
}

func (s *tunConn) checkForTimeouts() error {
	timer := time.NewTimer(tunConnTimeout)
	defer util.TimerStop(timer)
	defer s.close()
	for {
		select {
		case _, ok := <-s.alive:
			if !ok {
				return errors.New("connection closed")
			}
			util.TimerStop(timer)
			timer.Reset(tunConnTimeout)
		case <-timer.C:
			return errors.New("timed out")
		}
	}
}
