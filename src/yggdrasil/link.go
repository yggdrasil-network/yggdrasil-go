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
	"golang.org/x/net/proxy"

	"github.com/Arceliar/phony"
)

type link struct {
	core       *Core
	mutex      sync.RWMutex // protects interfaces below
	interfaces map[linkInfo]*linkInterface
	tcp        tcp // TCP interface support
	stopped    chan struct{}
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
	writeMsgs([][]byte) (int, error)
	close() error
	// These are temporary workarounds to stream semantics
	_sendMetaBytes([]byte) error
	_recvMetaBytes() ([]byte, error)
}

type linkInterface struct {
	name           string
	link           *link
	peer           *peer
	options        linkOptions
	msgIO          linkInterfaceMsgIO
	info           linkInfo
	incoming       bool
	force          bool
	closed         chan struct{}
	reader         linkReader  // Reads packets, notifies this linkInterface, passes packets to switch
	writer         linkWriter  // Writes packets, notifies this linkInterface
	phony.Inbox                // Protects the below
	sendTimer      *time.Timer // Fires to signal that sending is blocked
	keepAliveTimer *time.Timer // Fires to send keep-alive traffic
	stallTimer     *time.Timer // Fires to signal that no incoming traffic (including keep-alive) has been seen
	closeTimer     *time.Timer // Fires when the link has been idle so long we need to close it
	inSwitch       bool        // True if the switch is tracking this link
	stalled        bool        // True if we haven't been receiving any response traffic
	unstalled      bool        // False if an idle notification to the switch hasn't been sent because we stalled (or are first starting up)
}

type linkOptions struct {
	pinnedCurve25519Keys map[crypto.BoxPubKey]struct{}
	pinnedEd25519Keys    map[crypto.SigPubKey]struct{}
}

func (l *link) init(c *Core) error {
	l.core = c
	l.mutex.Lock()
	l.interfaces = make(map[linkInfo]*linkInterface)
	l.mutex.Unlock()
	l.stopped = make(chan struct{})

	if err := l.tcp.init(l); err != nil {
		c.log.Errorln("Failed to start TCP interface")
		return err
	}

	return nil
}

func (l *link) reconfigure() {
	l.tcp.reconfigure()
}

func (l *link) call(uri string, sintf string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("peer %s is not correctly formatted (%s)", uri, err)
	}
	pathtokens := strings.Split(strings.Trim(u.Path, "/"), "/")
	tcpOpts := tcpOptions{}
	if pubkeys, ok := u.Query()["curve25519"]; ok && len(pubkeys) > 0 {
		tcpOpts.pinnedCurve25519Keys = make(map[crypto.BoxPubKey]struct{})
		for _, pubkey := range pubkeys {
			if boxPub, err := hex.DecodeString(pubkey); err == nil {
				var boxPubKey crypto.BoxPubKey
				copy(boxPubKey[:], boxPub)
				tcpOpts.pinnedCurve25519Keys[boxPubKey] = struct{}{}
			}
		}
	}
	if pubkeys, ok := u.Query()["ed25519"]; ok && len(pubkeys) > 0 {
		tcpOpts.pinnedEd25519Keys = make(map[crypto.SigPubKey]struct{})
		for _, pubkey := range pubkeys {
			if sigPub, err := hex.DecodeString(pubkey); err == nil {
				var sigPubKey crypto.SigPubKey
				copy(sigPubKey[:], sigPub)
				tcpOpts.pinnedEd25519Keys[sigPubKey] = struct{}{}
			}
		}
	}
	switch u.Scheme {
	case "tcp":
		l.tcp.call(u.Host, tcpOpts, sintf)
	case "socks":
		tcpOpts.socksProxyAddr = u.Host
		if u.User != nil {
			tcpOpts.socksProxyAuth = &proxy.Auth{}
			tcpOpts.socksProxyAuth.User = u.User.Username()
			tcpOpts.socksProxyAuth.Password, _ = u.User.Password()
		}
		l.tcp.call(pathtokens[0], tcpOpts, sintf)
	case "tls":
		tcpOpts.upgrade = l.tcp.tls.forDialer
		l.tcp.call(u.Host, tcpOpts, sintf)
	default:
		return errors.New("unknown call scheme: " + u.Scheme)
	}
	return nil
}

func (l *link) listen(uri string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("listener %s is not correctly formatted (%s)", uri, err)
	}
	switch u.Scheme {
	case "tcp":
		_, err := l.tcp.listen(u.Host, nil)
		return err
	case "tls":
		_, err := l.tcp.listen(u.Host, l.tcp.tls.forListener)
		return err
	default:
		return errors.New("unknown listen scheme: " + u.Scheme)
	}
}

func (l *link) create(msgIO linkInterfaceMsgIO, name, linkType, local, remote string, incoming, force bool, options linkOptions) (*linkInterface, error) {
	// Technically anything unique would work for names, but let's pick something human readable, just for debugging
	intf := linkInterface{
		name:    name,
		link:    l,
		options: options,
		msgIO:   msgIO,
		info: linkInfo{
			linkType: linkType,
			local:    local,
			remote:   remote,
		},
		incoming: incoming,
		force:    force,
	}
	intf.writer.intf = &intf
	intf.reader.intf = &intf
	intf.reader.err = make(chan error)
	return &intf, nil
}

func (l *link) stop() error {
	close(l.stopped)
	if err := l.tcp.stop(); err != nil {
		return err
	}
	return nil
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
	// Check if the remote side matches the keys we expected. This is a bit of a weak
	// check - in future versions we really should check a signature or something like that.
	if pinned := intf.options.pinnedCurve25519Keys; pinned != nil {
		if _, allowed := pinned[meta.box]; !allowed {
			intf.link.core.log.Errorf("Failed to connect to node: %q sent curve25519 key that does not match pinned keys", intf.name)
			return fmt.Errorf("failed to connect: host sent curve25519 key that does not match pinned keys")
		}
	}
	if pinned := intf.options.pinnedEd25519Keys; pinned != nil {
		if _, allowed := pinned[meta.sig]; !allowed {
			intf.link.core.log.Errorf("Failed to connect to node: %q sent ed25519 key that does not match pinned keys", intf.name)
			return fmt.Errorf("failed to connect: host sent ed25519 key that does not match pinned keys")
		}
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
	}
	intf.closed = make(chan struct{})
	intf.link.interfaces[intf.info] = intf
	defer func() {
		intf.link.mutex.Lock()
		delete(intf.link.interfaces, intf.info)
		intf.link.mutex.Unlock()
		close(intf.closed)
	}()
	intf.link.core.log.Debugln("DEBUG: registered interface for", intf.name)
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
	intf.peer.out = func(msgs [][]byte) {
		intf.writer.sendFrom(intf.peer, msgs, false)
	}
	intf.peer.linkOut = func(bs []byte) {
		intf.writer.sendFrom(intf.peer, [][]byte{bs}, true)
	}
	themAddr := address.AddrForNodeID(crypto.GetNodeID(&intf.info.box))
	themAddrString := net.IP(themAddr[:]).String()
	themString := fmt.Sprintf("%s@%s", themAddrString, intf.info.remote)
	intf.link.core.log.Infof("Connected %s: %s, source %s",
		strings.ToUpper(intf.info.linkType), themString, intf.info.local)
	// Start things
	go intf.peer.start()
	intf.reader.Act(nil, intf.reader._read)
	// Wait for the reader to finish
	// TODO find a way to do this without keeping live goroutines around
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-intf.link.stopped:
			intf.msgIO.close()
		case <-done:
		}
	}()
	err = <-intf.reader.err
	// TODO don't report an error if it's just a 'use of closed network connection'
	if err != nil {
		intf.link.core.log.Infof("Disconnected %s: %s, source %s; error: %s",
			strings.ToUpper(intf.info.linkType), themString, intf.info.local, err)
	} else {
		intf.link.core.log.Infof("Disconnected %s: %s, source %s",
			strings.ToUpper(intf.info.linkType), themString, intf.info.local)
	}
	return err
}

////////////////////////////////////////////////////////////////////////////////

const (
	sendTime      = 1 * time.Second    // How long to wait before deciding a send is blocked
	keepAliveTime = 2 * time.Second    // How long to wait before sending a keep-alive response if we have no real traffic to send
	stallTime     = 6 * time.Second    // How long to wait for response traffic before deciding the connection has stalled
	closeTime     = 2 * switch_timeout // How long to wait before closing the link
)

// notify the intf that we're currently sending
func (intf *linkInterface) notifySending(size int, isLinkTraffic bool) {
	intf.Act(&intf.writer, func() {
		if !isLinkTraffic {
			intf.inSwitch = false
		}
		intf.sendTimer = time.AfterFunc(sendTime, intf.notifyBlockedSend)
		intf._cancelStallTimer()
	})
}

// called by an AfterFunc if we seem to be blocked in a send syscall for a long time
func (intf *linkInterface) _notifySyscall() {
	intf.link.core.switchTable.Act(intf, func() {
		intf.link.core.switchTable._sendingIn(intf.peer.port)
	})
}

// we just sent something, so cancel any pending timer to send keep-alive traffic
func (intf *linkInterface) _cancelStallTimer() {
	if intf.stallTimer != nil {
		intf.stallTimer.Stop()
		intf.stallTimer = nil
	}
}

// This gets called from a time.AfterFunc, and notifies the switch that we appear
// to have gotten blocked on a write, so the switch should start routing traffic
// through other links, if alternatives exist
func (intf *linkInterface) notifyBlockedSend() {
	intf.Act(nil, func() {
		if intf.sendTimer != nil {
			//As far as we know, we're still trying to send, and the timer fired.
			intf.link.core.switchTable.blockPeer(intf.peer.port)
		}
	})
}

// notify the intf that we've finished sending, returning the peer to the switch
func (intf *linkInterface) notifySent(size int, isLinkTraffic bool) {
	intf.Act(&intf.writer, func() {
		intf.sendTimer.Stop()
		intf.sendTimer = nil
		if !isLinkTraffic {
			intf._notifySwitch()
		}
		if size > 0 && intf.stallTimer == nil {
			intf.stallTimer = time.AfterFunc(stallTime, intf.notifyStalled)
		}
	})
}

// Notify the switch that we're ready for more traffic, assuming we're not in a stalled state
func (intf *linkInterface) _notifySwitch() {
	if !intf.inSwitch {
		if intf.stalled {
			intf.unstalled = false
		} else {
			intf.inSwitch = true
			intf.link.core.switchTable.Act(intf, func() {
				intf.link.core.switchTable._idleIn(intf.peer.port)
			})
		}
	}
}

// Set the peer as stalled, to prevent them from returning to the switch until a read succeeds
func (intf *linkInterface) notifyStalled() {
	intf.Act(nil, func() { // Sent from a time.AfterFunc
		if intf.stallTimer != nil {
			intf.stallTimer.Stop()
			intf.stallTimer = nil
			intf.stalled = true
			intf.link.core.switchTable.blockPeer(intf.peer.port)
		}
	})
}

// reset the close timer
func (intf *linkInterface) notifyReading() {
	intf.Act(&intf.reader, func() {
		if intf.closeTimer != nil {
			intf.closeTimer.Stop()
		}
		intf.closeTimer = time.AfterFunc(closeTime, func() { intf.msgIO.close() })
	})
}

// wake up the link if it was stalled, and (if size > 0) prepare to send keep-alive traffic
func (intf *linkInterface) notifyRead(size int) {
	intf.Act(&intf.reader, func() {
		if intf.stallTimer != nil {
			intf.stallTimer.Stop()
			intf.stallTimer = nil
		}
		intf.stalled = false
		if !intf.unstalled {
			intf._notifySwitch()
			intf.unstalled = true
		}
		if size > 0 && intf.stallTimer == nil {
			intf.stallTimer = time.AfterFunc(keepAliveTime, intf.notifyDoKeepAlive)
		}
	})
}

// We need to send keep-alive traffic now
func (intf *linkInterface) notifyDoKeepAlive() {
	intf.Act(nil, func() { // Sent from a time.AfterFunc
		if intf.stallTimer != nil {
			intf.stallTimer.Stop()
			intf.stallTimer = nil
			intf.writer.sendFrom(nil, [][]byte{nil}, true) // Empty keep-alive traffic
		}
	})
}

////////////////////////////////////////////////////////////////////////////////

type linkWriter struct {
	phony.Inbox
	intf *linkInterface
}

func (w *linkWriter) sendFrom(from phony.Actor, bss [][]byte, isLinkTraffic bool) {
	w.Act(from, func() {
		var size int
		for _, bs := range bss {
			size += len(bs)
		}
		w.intf.notifySending(size, isLinkTraffic)
		// start a timer that will fire if we get stuck in writeMsgs for an oddly long time
		var once sync.Once
		timer := time.AfterFunc(time.Millisecond, func() {
			// 1 ms is kind of arbitrary
			// the rationale is that this should be very long compared to a syscall
			// but it's still short compared to end-to-end latency or human perception
			once.Do(func() {
				w.intf.Act(nil, w.intf._notifySyscall)
			})
		})
		w.intf.msgIO.writeMsgs(bss)
		// Make sure we either stop the timer from doing anything or wait until it's done
		once.Do(func() { timer.Stop() })
		w.intf.notifySent(size, isLinkTraffic)
		// Cleanup
		for _, bs := range bss {
			util.PutBytes(bs)
		}
	})
}

////////////////////////////////////////////////////////////////////////////////

type linkReader struct {
	phony.Inbox
	intf *linkInterface
	err  chan error
}

func (r *linkReader) _read() {
	r.intf.notifyReading()
	msg, err := r.intf.msgIO.readMsg()
	r.intf.notifyRead(len(msg))
	if len(msg) > 0 {
		r.intf.peer.handlePacketFrom(r, msg)
	}
	if err != nil {
		if err != io.EOF {
			r.err <- err
		}
		close(r.err)
		return
	}
	// Now try to read again
	r.Act(nil, r._read)
}
