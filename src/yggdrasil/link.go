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

type linkInterface struct {
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

	return nil
}

func (l *link) create(fromlink chan []byte, tolink chan []byte /*boxPubKey *crypto.BoxPubKey, sigPubKey *crypto.SigPubKey*/, name string) (*linkInterface, error) {
	intf := linkInterface{
		link:     l,
		fromlink: fromlink,
		tolink:   tolink,
		shutdown: make(chan bool),
	}
	l.mutex.Lock()
	l.interfaces[name] = &intf
	l.mutex.Unlock()
	myLinkPub, myLinkPriv := crypto.NewBoxKeys()
	meta := version_getBaseMetadata()
	meta.box = l.core.boxPub
	meta.sig = l.core.sigPub
	meta.link = *myLinkPub
	metaBytes := meta.encode()
	tolink <- metaBytes
	metaBytes = <-fromlink
	meta = version_metadata{}
	if !meta.decode(metaBytes) || !meta.check() {
		return nil, errors.New("Metadata decode failure")
	}
	base := version_getBaseMetadata()
	if meta.ver > base.ver || meta.ver == base.ver && meta.minorVer > base.minorVer {
		return nil, errors.New("Failed to connect to node: " + name + " version: " + fmt.Sprintf("%d.%d", meta.ver, meta.minorVer))
	}
	shared := crypto.GetSharedKey(myLinkPriv, &meta.link)
	//shared := crypto.GetSharedKey(&l.core.boxPriv, boxPubKey)
	intf.peer = l.core.peers.newPeer(&meta.box, &meta.sig, shared, name)
	if intf.peer != nil {
		intf.peer.linkOut = make(chan []byte, 1) // protocol traffic
		intf.peer.out = func(msg []byte) {
			defer func() { recover() }()
			intf.tolink <- msg
		} // called by peer.sendPacket()
		l.core.switchTable.idleIn <- intf.peer.port // notify switch that we're idle
		intf.peer.close = func() {
			close(intf.fromlink)
			close(intf.tolink)
		}
		go intf.handler()
		go intf.peer.linkLoop()
		return &intf, nil
	}
	delete(l.interfaces, name)
	return nil, errors.New("l.core.peers.newPeer failed")
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
		return errors.New(fmt.Sprintf("Interface '%s' doesn't exist or already shutdown", identity))
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
