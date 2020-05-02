package yggdrasil

import (
	"errors"
	"github.com/Arceliar/phony"
)

type Simlink struct {
	phony.Inbox
	rch     chan []byte
	dest    *Simlink
	link    *linkInterface
	started bool
}

func (s *Simlink) readMsg() ([]byte, error) {
	bs := <-s.rch
	if bs != nil {
		return bs, nil
	} else {
		return nil, errors.New("read from closed Simlink")
	}
}

func (s *Simlink) _recvMetaBytes() ([]byte, error) {
	return s.readMsg()
}

func (s *Simlink) _sendMetaBytes(bs []byte) error {
	_, err := s.writeMsgs([][]byte{bs})
	return err
}

func (s *Simlink) close() error {
	close(s.rch)
	return nil
}

func (s *Simlink) writeMsgs(msgs [][]byte) (int, error) {
	if s.dest == nil {
		return 0, errors.New("write to unpaired Simlink")
	}
	var size int
	for _, msg := range msgs {
		size += len(msg)
		bs := append([]byte(nil), msg...)
		phony.Block(s, func() {
			s.dest.Act(s, func() {
				defer func() { recover() }()
				s.dest.rch <- bs
			})
		})
	}
	return size, nil
}

func (c *Core) NewSimlink() *Simlink {
	s := &Simlink{rch: make(chan []byte, 1)}
	n := "Simlink"
	s.link, _ = c.link.create(s, n, n, n, n, false, true)
	return s
}

func (s *Simlink) SetDestination(dest *Simlink) error {
	var err error
	phony.Block(s, func() {
		if s.dest != nil {
			err = errors.New("destination already set")
		} else {
			s.dest = dest
		}
	})
	return err
}

func (s *Simlink) Start() error {
	var err error
	phony.Block(s, func() {
		if s.started {
			err = errors.New("already started")
		} else {
			s.started = true
			go s.link.handler()
		}
	})
	return err
}
