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
}

func (l *awdl) init(c *Core) error {
	l.core = c
	l.mutex.Lock()
	l.interfaces = make(map[string]*awdlInterface)
	l.mutex.Unlock()

	return nil
}

func (l *awdl) create(fromAWDL chan []byte, toAWDL chan []byte /*boxPubKey *crypto.BoxPubKey, sigPubKey *crypto.SigPubKey*/, name string) (*awdlInterface, error) {
	intf := awdlInterface{
		awdl:     l,
		fromAWDL: fromAWDL,
		toAWDL:   toAWDL,
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
	l.core.log.Println("toAWDL <- metaBytes")
	toAWDL <- metaBytes
	l.core.log.Println("metaBytes = <-fromAWDL")
	metaBytes = <-fromAWDL
	l.core.log.Println("version_metadata{}")
	meta = version_metadata{}
	if !meta.decode(metaBytes) || !meta.check() {
		return nil, errors.New("Metadata decode failure")
	}
	l.core.log.Println("version_getBaseMetadata{}")
	base := version_getBaseMetadata()
	if meta.ver > base.ver || meta.ver == base.ver && meta.minorVer > base.minorVer {
		return nil, errors.New("Failed to connect to node: " + name + " version: " + fmt.Sprintf("%d.%d", meta.ver, meta.minorVer))
	}
	l.core.log.Println("crypto.GetSharedKey")
	shared := crypto.GetSharedKey(myLinkPriv, &meta.link)
	//shared := crypto.GetSharedKey(&l.core.boxPriv, boxPubKey)
	l.core.log.Println("l.core.peers.newPeer")
	intf.peer = l.core.peers.newPeer(&meta.box, &meta.sig, shared, name)
	if intf.peer != nil {
		intf.peer.linkOut = make(chan []byte, 1) // protocol traffic
		intf.peer.out = func(msg []byte) {
			defer func() { recover() }()
			intf.toAWDL <- msg
		} // called by peer.sendPacket()
		l.core.switchTable.idleIn <- intf.peer.port // notify switch that we're idle
		intf.peer.close = func() {
			close(intf.fromAWDL)
			close(intf.toAWDL)
		}
		go intf.handler()
		go intf.peer.linkLoop()
		return &intf, nil
	}
	delete(l.interfaces, name)
	return nil, errors.New("l.core.peers.newPeer failed")
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

func (ai *awdlInterface) handler() {
	send := func(msg []byte) {
		ai.toAWDL <- msg
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
		case r := <-ai.fromAWDL:
			ai.peer.handlePacket(r)
			ai.awdl.core.switchTable.idleIn <- ai.peer.port
		case <-ai.shutdown:
			return
		}
	}
}
