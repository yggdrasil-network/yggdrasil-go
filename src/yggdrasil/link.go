package yggdrasil

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"

	//"sync/atomic"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

type link struct {
	core        *Core
	reconfigure chan chan error
	mutex       sync.RWMutex // protects interfaces below
	interfaces  map[linkInfo]*linkInterface
	tcp         tcp // TCP interface support
	// TODO timeout (to remove from switch), read from config.ReadTimeout
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
	name     string
	link     *link
	peer     *peer
	msgIO    linkInterfaceMsgIO
	info     linkInfo
	incoming bool
	force    bool
	closed   chan struct{}
}

func (l *link) init(c *Core) error {
	l.core = c
	l.mutex.Lock()
	l.interfaces = make(map[linkInfo]*linkInterface)
	l.reconfigure = make(chan chan error)
	l.mutex.Unlock()

	if err := l.tcp.init(l); err != nil {
		c.log.Errorln("Failed to start TCP interface")
		return err
	}

	go func() {
		for {
			e := <-l.reconfigure
			tcpresponse := make(chan error)
			l.tcp.reconfigure <- tcpresponse
			if err := <-tcpresponse; err != nil {
				e <- err
				continue
			}
			e <- nil
		}
	}()

	return nil
}

func (l *link) call(uri string, sintf string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return err
	}
	pathtokens := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch u.Scheme {
	case "tcp":
		l.tcp.call(u.Host, nil, sintf)
	case "socks":
		l.tcp.call(pathtokens[0], u.Host, sintf)
	default:
		return errors.New("unknown call scheme: " + u.Scheme)
	}
	return nil
}

func (l *link) listen(uri string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "tcp":
		_, err := l.tcp.listen(u.Host)
		return err
	default:
		return errors.New("unknown listen scheme: " + u.Scheme)
	}
}

func (l *link) create(msgIO linkInterfaceMsgIO, name, linkType, local, remote string, incoming, force bool) (*linkInterface, error) {
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
		incoming: incoming,
		force:    force,
	}
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
	var err error
	if !util.FuncTimeout(func() { err = intf.msgIO._sendMetaBytes(metaBytes) }, 30*time.Second) {
		return errors.New("timeout on metadata send")
	}
	if err != nil {
		return err
	}
	if !util.FuncTimeout(func() { metaBytes, err = intf.msgIO._recvMetaBytes() }, 30*time.Second) {
		return errors.New("timeout on metadata recv")
	}
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
	// Check if we're authorized to connect to this key / IP
	if intf.incoming && !intf.force && !intf.link.core.peers.isAllowedEncryptionPublicKey(&meta.box) {
		intf.link.core.log.Warnf("%s connection from %s forbidden: AllowedEncryptionPublicKeys does not contain key %s",
			strings.ToUpper(intf.info.linkType), intf.info.remote, hex.EncodeToString(meta.box[:]))
		intf.msgIO.close()
		return nil
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
		if !intf.incoming {
			// Block outgoing connection attempts until the existing connection closes
			<-oldIntf.closed
		}
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
	intf.peer = intf.link.core.peers.newPeer(&meta.box, &meta.sig, shared, intf, func() { intf.msgIO.close() })
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
	themAddr := address.AddrForNodeID(crypto.GetNodeID(&intf.info.box))
	themAddrString := net.IP(themAddr[:]).String()
	themString := fmt.Sprintf("%s@%s", themAddrString, intf.info.remote)
	intf.link.core.log.Infof("Connected %s: %s, source %s",
		strings.ToUpper(intf.info.linkType), themString, intf.info.local)
	// Start the link loop
	go intf.peer.linkLoop()
	// Start the writer
	signalReady := make(chan struct{}, 1)
	signalSent := make(chan bool, 1)
	sendAck := make(chan struct{}, 1)
	sendBlocked := time.NewTimer(time.Second)
	defer util.TimerStop(sendBlocked)
	util.TimerStop(sendBlocked)
	go func() {
		defer close(signalReady)
		defer close(signalSent)
		interval := 4 * time.Second
		tcpTimer := time.NewTimer(interval) // used for backwards compat with old tcp
		defer util.TimerStop(tcpTimer)
		send := func(bs []byte) {
			sendBlocked.Reset(time.Second)
			intf.msgIO.writeMsg(bs)
			util.TimerStop(sendBlocked)
			select {
			case signalSent <- len(bs) > 0:
			default:
			}
		}
		for {
			// First try to send any link protocol traffic
			select {
			case msg := <-intf.peer.linkOut:
				send(msg)
				continue
			default:
			}
			// No protocol traffic to send, so reset the timer
			util.TimerStop(tcpTimer)
			tcpTimer.Reset(interval)
			// Now block until something is ready or the timer triggers keepalive traffic
			select {
			case <-tcpTimer.C:
				intf.link.core.log.Tracef("Sending (legacy) keep-alive to %s: %s, source %s",
					strings.ToUpper(intf.info.linkType), themString, intf.info.local)
				send(nil)
			case <-sendAck:
				intf.link.core.log.Tracef("Sending ack to %s: %s, source %s",
					strings.ToUpper(intf.info.linkType), themString, intf.info.local)
				send(nil)
			case msg := <-intf.peer.linkOut:
				send(msg)
			case msg, ok := <-out:
				if !ok {
					return
				}
				send(msg)
				util.PutBytes(msg)
				select {
				case signalReady <- struct{}{}:
				default:
				}
				//intf.link.core.log.Tracef("Sending packet to %s: %s, source %s",
				//	strings.ToUpper(intf.info.linkType), themString, intf.info.local)
			}
		}
	}()
	//intf.link.core.switchTable.idleIn <- intf.peer.port // notify switch that we're idle
	// Used to enable/disable activity in the switch
	signalAlive := make(chan bool, 1) // True = real packet, false = keep-alive
	defer close(signalAlive)
	ret := make(chan error, 1) // How we signal the return value when multiple goroutines are involved
	go func() {
		var isAlive bool
		var isReady bool
		var sendTimerRunning bool
		var recvTimerRunning bool
		recvTime := 6 * time.Second     // TODO set to ReadTimeout from the config, reset if it gets changed
		closeTime := 2 * switch_timeout // TODO or maybe this makes more sense for ReadTimeout?...
		sendTime := time.Second
		sendTimer := time.NewTimer(sendTime)
		defer util.TimerStop(sendTimer)
		recvTimer := time.NewTimer(recvTime)
		defer util.TimerStop(recvTimer)
		closeTimer := time.NewTimer(closeTime)
		defer util.TimerStop(closeTimer)
		for {
			//intf.link.core.log.Debugf("State of %s: %s, source %s :: isAlive %t isReady %t sendTimerRunning %t recvTimerRunning %t",
			//	strings.ToUpper(intf.info.linkType), themString, intf.info.local,
			//	isAlive, isReady, sendTimerRunning, recvTimerRunning)
			select {
			case gotMsg, ok := <-signalAlive:
				if !ok {
					return
				}
				util.TimerStop(closeTimer)
				closeTimer.Reset(closeTime)
				util.TimerStop(recvTimer)
				recvTimerRunning = false
				isAlive = true
				if !isReady {
					// (Re-)enable in the switch
					intf.link.core.switchTable.idleIn <- intf.peer.port
					isReady = true
				}
				if gotMsg && !sendTimerRunning {
					// We got a message
					// Start a timer, if it expires then send a 0-sized ack to let them know we're alive
					util.TimerStop(sendTimer)
					sendTimer.Reset(sendTime)
					sendTimerRunning = true
				}
				if !gotMsg {
					intf.link.core.log.Tracef("Received ack from %s: %s, source %s",
						strings.ToUpper(intf.info.linkType), themString, intf.info.local)
				}
			case sentMsg, ok := <-signalSent:
				// Stop any running ack timer
				if !ok {
					return
				}
				util.TimerStop(sendTimer)
				sendTimerRunning = false
				if sentMsg && !recvTimerRunning {
					// We sent a message
					// Start a timer, if it expires and we haven't gotten any return traffic (including a 0-sized ack), then assume there's a problem
					util.TimerStop(recvTimer)
					recvTimer.Reset(recvTime)
					recvTimerRunning = true
				}
			case _, ok := <-signalReady:
				if !ok {
					return
				}
				if !isAlive {
					// Disable in the switch
					isReady = false
				} else {
					// Keep enabled in the switch
					intf.link.core.switchTable.idleIn <- intf.peer.port
					isReady = true
				}
			case <-sendBlocked.C:
				// We blocked while trying to send something
				isReady = false
				intf.link.core.switchTable.blockPeer(intf.peer.port)
			case <-sendTimer.C:
				// We haven't sent anything, so signal a send of a 0 packet to let them know we're alive
				select {
				case sendAck <- struct{}{}:
				default:
				}
			case <-recvTimer.C:
				// We haven't received anything, so assume there's a problem and don't return this node to the switch until they start responding
				isAlive = false
				intf.link.core.switchTable.blockPeer(intf.peer.port)
			case <-closeTimer.C:
				// We haven't received anything in a really long time, so things have died at the switch level and then some...
				// Just close the connection at this point...
				select {
				case ret <- errors.New("timeout"):
				default:
				}
				intf.msgIO.close()
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
			if err != io.EOF {
				select {
				case ret <- err:
				default:
				}
			}
			break
		}
		select {
		case signalAlive <- len(msg) > 0:
		default:
		}
	}
	////////////////////////////////////////////////////////////////////////////////
	// Remember to set `err` to something useful before returning
	select {
	case err = <-ret:
		intf.link.core.log.Infof("Disconnected %s: %s, source %s; error: %s",
			strings.ToUpper(intf.info.linkType), themString, intf.info.local, err)
	default:
		err = nil
		intf.link.core.log.Infof("Disconnected %s: %s, source %s",
			strings.ToUpper(intf.info.linkType), themString, intf.info.local)
	}
	return err
}
