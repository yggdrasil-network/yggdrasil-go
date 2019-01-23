package yggdrasil

import (
	"errors"
	"fmt"
	"sync"
	//"sync/atomic"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

type link struct {
	core       *Core
	mutex      sync.RWMutex // protects interfaces below
	interfaces map[linkInfo]*linkInterface
}

type linkInfo struct {
	box      crypto.BoxPubKey // Their encryption key
	sig      crypto.SigPubKey // Their signing key
	linkType string           // Type of link, e.g. TCP, AWDL
	local    string           // Local name or address
	remote   string           // Remote name or address
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
	name   string
	link   *link
	peer   *peer
	msgIO  linkInterfaceMsgIO
	info   linkInfo
	closed chan struct{}
}

func (l *link) init(c *Core) error {
	l.core = c
	l.mutex.Lock()
	l.interfaces = make(map[linkInfo]*linkInterface)
	l.mutex.Unlock()

	if err := l.core.awdl.init(c); err != nil {
		l.core.log.Println("Failed to start AWDL interface")
		return err
	}

	return nil
}

func (l *link) create(msgIO linkInterfaceMsgIO, name, linkType, local, remote string) (*linkInterface, error) {
	// Technically anything unique would work for names, but lets pick something human readable, just for debugging
	intf := linkInterface{
		name:  name,
		link:  l,
		msgIO: msgIO,
		info: linkInfo{
			linkType: linkType,
			local:    local,
			remote:   remote,
		},
	}
	//l.interfaces[intf.name] = &intf
	//go intf.start()
	return &intf, nil
}

func (intf *linkInterface) handler() error {
	// TODO split some of this into shorter functions, so it's easier to read, and for the FIXME duplicate peer issue mentioned later
	myLinkPub, myLinkPriv := crypto.NewBoxKeys()
	meta := version_getBaseMetadata()
	meta.box = intf.link.core.boxPub
	meta.sig = intf.link.core.sigPub
	meta.link = *myLinkPub
	metaBytes := meta.encode()
	// TODO timeouts on send/recv (goroutine for send/recv, channel select w/ timer)
	err := intf.msgIO._sendMetaBytes(metaBytes)
	if err != nil {
		return err
	}
	metaBytes, err = intf.msgIO._recvMetaBytes()
	if err != nil {
		return err
	}
	meta = version_metadata{}
	if !meta.decode(metaBytes) || !meta.check() {
		return errors.New("failed to decode metadata")
	}
	base := version_getBaseMetadata()
	if meta.ver > base.ver || meta.ver == base.ver && meta.minorVer > base.minorVer {
		intf.link.core.log.Println("Failed to connect to node: " + intf.name + " version: " + fmt.Sprintf("%d.%d", meta.ver, meta.minorVer))
		return errors.New("failed to connect: wrong version")
	}
	// Check if we already have a link to this node
	intf.info.box = meta.box
	intf.info.sig = meta.sig
	intf.link.mutex.Lock()
	if oldIntf, isIn := intf.link.interfaces[intf.info]; isIn {
		intf.link.mutex.Unlock()
		// FIXME we should really return an error and let the caller block instead
		// That lets them do things like close connections before blocking
		intf.link.core.log.Println("DEBUG: found existing interface for", intf.name)
		<-oldIntf.closed
		return nil
	} else {
		intf.closed = make(chan struct{})
		intf.link.interfaces[intf.info] = intf
		defer close(intf.closed)
		intf.link.core.log.Println("DEBUG: registered interface for", intf.name)
	}
	intf.link.mutex.Unlock()
	// Create peer
	shared := crypto.GetSharedKey(myLinkPriv, &meta.link)
	intf.peer = intf.link.core.peers.newPeer(&meta.box, &meta.sig, shared, intf.name)
	if intf.peer == nil {
		return errors.New("failed to create peer")
	}
	defer func() {
		// More cleanup can go here
		intf.link.core.peers.removePeer(intf.peer.port)
	}()
	// Finish setting up the peer struct
	out := make(chan []byte, 1)
	defer close(out)
	intf.peer.out = func(msg []byte) {
		defer func() { recover() }()
		out <- msg
	}
	intf.peer.linkOut = make(chan []byte, 1)
	intf.peer.close = func() { intf.msgIO.close() }
	go intf.peer.linkLoop()
	// Start the writer
	go func() {
		interval := 4 * time.Second
		timer := time.NewTimer(interval)
		clearTimer := func() {
			if !timer.Stop() {
				<-timer.C
			}
		}
		defer clearTimer()
		for {
			// First try to send any link protocol traffic
			select {
			case msg := <-intf.peer.linkOut:
				intf.msgIO.writeMsg(msg)
				continue
			default:
			}
			// No protocol traffic to send, so reset the timer
			clearTimer()
			timer.Reset(interval)
			// Now block until something is ready or the timer triggers keepalive traffic
			select {
			case <-timer.C:
				intf.msgIO.writeMsg(nil)
			case msg := <-intf.peer.linkOut:
				intf.msgIO.writeMsg(msg)
			case msg, ok := <-out:
				if !ok {
					return
				}
				intf.msgIO.writeMsg(msg)
				util.PutBytes(msg)
				if true {
					// TODO *don't* do this if we're not reading any traffic
					// In such a case, the reader is responsible for resetting it the next time we read something
					intf.link.core.switchTable.idleIn <- intf.peer.port
				}
			}
		}
	}()
	intf.link.core.switchTable.idleIn <- intf.peer.port // notify switch that we're idle
	// Run reader loop
	for {
		msg, err := intf.msgIO.readMsg()
		if len(msg) > 0 {
			intf.peer.handlePacket(msg)
		}
		if err != nil {
			return err
		}
	}
	////////////////////////////////////////////////////////////////////////////////
	return nil
}

/*
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
*/
