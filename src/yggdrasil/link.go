package yggdrasil

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

type link struct {
	core       *Core
	mutex      sync.RWMutex // protects interfaces below
	interfaces map[string]*linkInterface
}

type linkInterfaceMsgIO interface {
	readMsg() ([]byte, error)
	writeMsg([]byte) (int, error)
	close() error
	// These are temporary workarounds to stream semantics
	_sendMetaBytes([]byte) error
	_recvMetaBytes() ([]byte, error)
}

type linkInterface struct {
	name     string
	link     *link
	fromlink chan []byte
	tolink   chan []byte
	shutdown chan bool
	peer     *peer
	stream   stream
}

func (l *link) init(c *Core) error {
	l.core = c
	l.mutex.Lock()
	l.interfaces = make(map[string]*linkInterface)
	l.mutex.Unlock()

	if err := l.core.awdl.init(c); err != nil {
		l.core.log.Println("Failed to start AWDL interface")
		return err
	}

	return nil
}

func (l *link) create(fromlink chan []byte, tolink chan []byte, name string) (*linkInterface, error) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if _, ok := l.interfaces[name]; ok {
		return nil, errors.New("Interface with this name already exists")
	}
	intf := linkInterface{
		name:     name,
		link:     l,
		fromlink: fromlink,
		tolink:   tolink,
		shutdown: make(chan bool),
	}
	l.interfaces[intf.name] = &intf
	go intf.start()
	return &intf, nil
}

func (intf *linkInterface) start() {
	myLinkPub, myLinkPriv := crypto.NewBoxKeys()
	meta := version_getBaseMetadata()
	meta.box = intf.link.core.boxPub
	meta.sig = intf.link.core.sigPub
	meta.link = *myLinkPub
	metaBytes := meta.encode()
	//intf.link.core.log.Println("start: intf.tolink <- metaBytes")
	intf.tolink <- metaBytes
	//intf.link.core.log.Println("finish: intf.tolink <- metaBytes")
	//intf.link.core.log.Println("start: metaBytes = <-intf.fromlink")
	metaBytes = <-intf.fromlink
	//intf.link.core.log.Println("finish: metaBytes = <-intf.fromlink")
	meta = version_metadata{}
	if !meta.decode(metaBytes) || !meta.check() {
		intf.link.core.log.Println("Metadata decode failure")
		return
	}
	base := version_getBaseMetadata()
	if meta.ver > base.ver || meta.ver == base.ver && meta.minorVer > base.minorVer {
		intf.link.core.log.Println("Failed to connect to node: " + intf.name + " version: " + fmt.Sprintf("%d.%d", meta.ver, meta.minorVer))
		return
	}
	shared := crypto.GetSharedKey(myLinkPriv, &meta.link)
	intf.peer = intf.link.core.peers.newPeer(&meta.box, &meta.sig, shared, intf.name)
	if intf.peer == nil {
		intf.link.mutex.Lock()
		delete(intf.link.interfaces, intf.name)
		intf.link.mutex.Unlock()
		return
	}
	intf.peer.linkOut = make(chan []byte, 1) // protocol traffic
	intf.peer.out = func(msg []byte) {
		defer func() { recover() }()
		intf.tolink <- msg
	} // called by peer.sendPacket()
	intf.link.core.switchTable.idleIn <- intf.peer.port // notify switch that we're idle
	intf.peer.close = func() {
		close(intf.fromlink)
		close(intf.tolink)
	}
	go intf.handler()
	go intf.peer.linkLoop()
}

func (l *link) getInterface(identity string) *linkInterface {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	if intf, ok := l.interfaces[identity]; ok {
		return intf
	}
	return nil
}

func (l *link) shutdown(identity string) error {
	if intf, ok := l.interfaces[identity]; ok {
		intf.shutdown <- true
		l.core.peers.removePeer(intf.peer.port)
		l.mutex.Lock()
		delete(l.interfaces, identity)
		l.mutex.Unlock()
		return nil
	} else {
		return fmt.Errorf("interface '%s' doesn't exist or already shutdown", identity)
	}
}

func (ai *linkInterface) handler() {
	send := func(msg []byte) {
		ai.tolink <- msg
		atomic.AddUint64(&ai.peer.bytesSent, uint64(len(msg)))
		util.PutBytes(msg)
	}
	for {
		timerInterval := tcp_ping_interval
		timer := time.NewTimer(timerInterval)
		defer timer.Stop()
		select {
		case p := <-ai.peer.linkOut:
			send(p)
			continue
		default:
		}
		timer.Stop()
		select {
		case <-timer.C:
		default:
		}
		timer.Reset(timerInterval)
		select {
		case _ = <-timer.C:
			send([]byte{})
		case p := <-ai.peer.linkOut:
			send(p)
			continue
		case r := <-ai.fromlink:
			ai.peer.handlePacket(r)
			ai.link.core.switchTable.idleIn <- ai.peer.port
		case <-ai.shutdown:
			return
		}
	}
}
