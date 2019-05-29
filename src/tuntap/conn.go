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

func (s *tunConn) reader() error {
	select {
	case _, ok := <-s.stop:
		if !ok {
			return errors.New("session was already closed")
		}
	default:
	}
	var n int
	var err error
	read := make(chan bool)
	b := make([]byte, 65535)
	for {
		go func() {
			// TODO don't start a new goroutine for every packet read, this is probably a big part of the slowdowns we saw when refactoring
			if n, err = s.conn.Read(b); err != nil {
				s.tun.log.Errorln(s.conn.String(), "TUN/TAP conn read error:", err)
				return
			}
			read <- true
		}()
		select {
		case <-read:
			if n > 0 {
				bs := append(util.GetBytes(), b[:n]...)
				select {
				case s.tun.send <- bs:
				default:
					util.PutBytes(bs)
				}
			}
			s.stillAlive() // TODO? Only stay alive if we read >0 bytes?
		case <-s.stop:
			s.tun.log.Debugln("Stopping conn reader for", s)
			return nil
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
	for {
		select {
		case <-s.stop:
			s.tun.log.Debugln("Stopping conn writer for", s)
			return nil
		case b, ok := <-s.send:
			if !ok {
				return errors.New("send closed")
			}
			// TODO write timeout and close
			if _, err := s.conn.Write(b); err != nil {
				e, eok := err.(yggdrasil.ConnError)
				if !eok {
					s.tun.log.Errorln(s.conn.String(), "TUN/TAP generic write error:", err)
				} else if ispackettoobig, maxsize := e.PacketTooBig(); ispackettoobig {
					// TODO: This currently isn't aware of IPv4 for CKR
					ptb := &icmp.PacketTooBig{
						MTU:  int(maxsize),
						Data: b[:900],
					}
					if packet, err := CreateICMPv6(b[8:24], b[24:40], ipv6.ICMPTypePacketTooBig, 0, ptb); err == nil {
						s.tun.send <- packet
					}
				} else {
					s.tun.log.Errorln(s.conn.String(), "TUN/TAP conn write error:", err)
				}
			}
			util.PutBytes(b)
			s.stillAlive()
		}
	}
}

func (s *tunConn) stillAlive() {
	select {
	case s.alive <- struct{}{}:
	default:
	}
}

func (s *tunConn) checkForTimeouts() error {
	const timeout = 2 * time.Minute
	timer := time.NewTimer(timeout)
	defer util.TimerStop(timer)
	defer s.close()
	for {
		select {
		case _, ok := <-s.alive:
			if !ok {
				return errors.New("connection closed")
			}
			util.TimerStop(timer)
			timer.Reset(timeout)
		case <-timer.C:
			return errors.New("timed out")
		}
	}
}
