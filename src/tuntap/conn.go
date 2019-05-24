package tuntap

import (
	"errors"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

type tunConn struct {
	tun  *TunAdapter
	conn *yggdrasil.Conn
	addr address.Address
	snet address.Subnet
	send chan []byte
	stop chan interface{}
}

func (s *tunConn) close() {
	s.tun.mutex.Lock()
	s._close_nomutex()
	s.tun.mutex.Unlock()
}

func (s *tunConn) _close_nomutex() {
	delete(s.tun.addrToConn, s.addr)
	delete(s.tun.subnetToConn, s.snet)
	close(s.stop)
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
			// TODO read timeout and close
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
				s.tun.log.Errorln(s.conn.String(), "TUN/TAP conn write error:", err)
			}
			util.PutBytes(b)
		}
	}
}
