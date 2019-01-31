package yggdrasil

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	//"sync/atomic"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

type link struct {
	core       *Core
	mutex      sync.RWMutex // protects interfaces below
	interfaces map[linkInfo]*linkInterface
	awdl       awdl // AWDL interface support
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

	if err := l.awdl.init(l); err != nil {
		l.core.log.Errorln("Failed to start AWDL interface")
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
		intf.link.core.log.Errorln("Failed to connect to node: " + intf.name + " version: " + fmt.Sprintf("%d.%d", meta.ver, meta.minorVer))
		return errors.New("failed to connect: wrong version")
	}
	// Check if we already have a link to this node
	intf.info.box = meta.box
	intf.info.sig = meta.sig
	intf.link.mutex.Lock()
	if oldIntf, isIn := intf.link.interfaces[intf.info]; isIn {
		intf.link.mutex.Unlock()
		// FIXME we should really return an error and let the caller block instead
		// That lets them do things like close connections on its own, avoid printing a connection message in the first place, etc.
		intf.link.core.log.Debugln("DEBUG: found existing interface for", intf.name)
		intf.msgIO.close()
		<-oldIntf.closed
		return nil
	} else {
		intf.closed = make(chan struct{})
		intf.link.interfaces[intf.info] = intf
		defer func() {
			intf.link.mutex.Lock()
			delete(intf.link.interfaces, intf.info)
			intf.link.mutex.Unlock()
			close(intf.closed)
		}()
		intf.link.core.log.Debugln("DEBUG: registered interface for", intf.name)
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
	intf.peer.close = func() {
		intf.msgIO.close()
		// Make output
		themAddr := address.AddrForNodeID(crypto.GetNodeID(&intf.info.box))
		themAddrString := net.IP(themAddr[:]).String()
		themString := fmt.Sprintf("%s@%s", themAddrString, intf.info.remote)
		intf.link.core.log.Infof("Disconnected %s: %s, source %s",
			strings.ToUpper(intf.info.linkType), themString, intf.info.local)
	}
	// Make output
	themAddr := address.AddrForNodeID(crypto.GetNodeID(&intf.info.box))
	themAddrString := net.IP(themAddr[:]).String()
	themString := fmt.Sprintf("%s@%s", themAddrString, intf.info.remote)
	intf.link.core.log.Infof("Connected %s: %s, source %s",
		strings.ToUpper(intf.info.linkType), themString, intf.info.local)
	// Start the link loop
	go intf.peer.linkLoop()
	// Start the writer
	signalReady := make(chan struct{}, 1)
	go func() {
		defer close(signalReady)
		interval := 4 * time.Second
		timer := time.NewTimer(interval)
		clearTimer := func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
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
				select {
				case signalReady <- struct{}{}:
				default:
				}
			}
		}
	}()
	//intf.link.core.switchTable.idleIn <- intf.peer.port // notify switch that we're idle
	// Used to enable/disable activity in the switch
	signalAlive := make(chan struct{}, 1)
	defer close(signalAlive)
	go func() {
		var isAlive bool
		var isReady bool
		interval := 6 * time.Second // TODO set to ReadTimeout from the config, reset if it gets changed
		timer := time.NewTimer(interval)
		clearTimer := func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		defer clearTimer()
		for {
			clearTimer()
			timer.Reset(interval)
			select {
			case _, ok := <-signalAlive:
				if !ok {
					return
				}
				if !isAlive {
					isAlive = true
					if !isReady {
						// (Re-)enable in the switch
						isReady = true
						intf.link.core.switchTable.idleIn <- intf.peer.port
					}
				}
			case _, ok := <-signalReady:
				if !ok {
					return
				}
				if !isAlive || !isReady {
					// Disable in the switch
					isReady = false
				} else {
					// Keep enabled in the switch
					intf.link.core.switchTable.idleIn <- intf.peer.port
				}
			case <-timer.C:
				isAlive = false
			}
		}
	}()
	// Run reader loop
	for {
		msg, err := intf.msgIO.readMsg()
		if len(msg) > 0 {
			intf.peer.handlePacket(msg)
		}
		if err != nil {
			return err
		}
		select {
		case signalAlive <- struct{}{}:
		default:
		}
	}
	////////////////////////////////////////////////////////////////////////////////
	return nil
}
