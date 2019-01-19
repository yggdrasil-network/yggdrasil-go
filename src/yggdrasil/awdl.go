package yggdrasil

import (
	"fmt"
	"sync"
)

type awdl struct {
	core       *Core
	mutex      sync.RWMutex // protects interfaces below
	interfaces map[string]*awdlInterface
}

type awdlInterface struct {
	awdl     *awdl
	fromAWDL chan []byte
	toAWDL   chan []byte
	shutdown chan bool
	peer     *peer
	link     *linkInterface
}

func (l *awdl) init(c *Core) error {
	l.core = c
	l.mutex.Lock()
	l.interfaces = make(map[string]*awdlInterface)
	l.mutex.Unlock()

	return nil
}

func (l *awdl) create(fromAWDL chan []byte, toAWDL chan []byte, name string) (*awdlInterface, error) {
	link, err := l.core.link.create(fromAWDL, toAWDL, name)
	if err != nil {
		return nil, err
	}
	intf := awdlInterface{
		awdl:     l,
		link:     link,
		fromAWDL: fromAWDL,
		toAWDL:   toAWDL,
		shutdown: make(chan bool),
	}
	l.mutex.Lock()
	l.interfaces[name] = &intf
	l.mutex.Unlock()
	return &intf, nil
}

func (l *awdl) getInterface(identity string) *awdlInterface {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	if intf, ok := l.interfaces[identity]; ok {
		return intf
	}
	return nil
}

func (l *awdl) shutdown(identity string) error {
	if err := l.core.link.shutdown(identity); err != nil {
		return err
	}
	if intf, ok := l.interfaces[identity]; ok {
		intf.shutdown <- true
		l.mutex.Lock()
		delete(l.interfaces, identity)
		l.mutex.Unlock()
		return nil
	}
	return fmt.Errorf("interface '%s' doesn't exist or already shutdown", identity)
}

func (ai *awdlInterface) handler() {
	for {
		select {
		case <-ai.shutdown:
			return
		case <-ai.link.shutdown:
			return
		case in := <-ai.fromAWDL:
			ai.link.fromlink <- in
		case out := <-ai.link.tolink:
			ai.toAWDL <- out
		}
	}
}
