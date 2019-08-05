package tuntap

import (
	"errors"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
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
			// TODO write timeout and close
			if err := s.conn.WriteNoCopy(bs); err != nil {
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
