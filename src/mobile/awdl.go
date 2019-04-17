package mobile

/*
import (
	"errors"
	"io"
	"sync"
)

type awdl struct {
	link        *link
	reconfigure chan chan error
	mutex       sync.RWMutex // protects interfaces below
	interfaces  map[string]*awdlInterface
}

type awdlInterface struct {
	linkif *linkInterface
	rwc    awdlReadWriteCloser
	peer   *peer
	stream stream
}

type awdlReadWriteCloser struct {
	fromAWDL chan []byte
	toAWDL   chan []byte
}

func (c awdlReadWriteCloser) Read(p []byte) (n int, err error) {
	if packet, ok := <-c.fromAWDL; ok {
		n = copy(p, packet)
		return n, nil
	}
	return 0, io.EOF
}

func (c awdlReadWriteCloser) Write(p []byte) (n int, err error) {
	var pc []byte
	pc = append(pc, p...)
	c.toAWDL <- pc
	return len(pc), nil
}

func (c awdlReadWriteCloser) Close() error {
	close(c.fromAWDL)
	close(c.toAWDL)
	return nil
}

func (a *awdl) init(l *link) error {
	a.link = l
	a.mutex.Lock()
	a.interfaces = make(map[string]*awdlInterface)
	a.reconfigure = make(chan chan error, 1)
	a.mutex.Unlock()

	go func() {
		for e := range a.reconfigure {
			e <- nil
		}
	}()

	return nil
}

func (a *awdl) create(name, local, remote string, incoming bool) (*awdlInterface, error) {
	rwc := awdlReadWriteCloser{
		fromAWDL: make(chan []byte, 1),
		toAWDL:   make(chan []byte, 1),
	}
	s := stream{}
	s.init(rwc)
	linkif, err := a.link.create(&s, name, "awdl", local, remote, incoming, true)
	if err != nil {
		return nil, err
	}
	intf := awdlInterface{
		linkif: linkif,
		rwc:    rwc,
	}
	a.mutex.Lock()
	a.interfaces[name] = &intf
	a.mutex.Unlock()
	go intf.linkif.handler()
	return &intf, nil
}

func (a *awdl) getInterface(identity string) *awdlInterface {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	if intf, ok := a.interfaces[identity]; ok {
		return intf
	}
	return nil
}

func (a *awdl) shutdown(identity string) error {
	if intf, ok := a.interfaces[identity]; ok {
		close(intf.linkif.closed)
		intf.rwc.Close()
		a.mutex.Lock()
		delete(a.interfaces, identity)
		a.mutex.Unlock()
		return nil
	}
	return errors.New("Interface not found or already closed")
}
*/
