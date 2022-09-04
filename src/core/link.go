package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	//"sync/atomic"
	"time"

	"sync/atomic"

	"github.com/Arceliar/phony"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
	//"github.com/Arceliar/phony" // TODO? use instead of mutexes
)

type links struct {
	phony.Inbox
	core   *Core
	tcp    *linkTCP           // TCP interface support
	tls    *linkTLS           // TLS interface support
	unix   *linkUNIX          // UNIX interface support
	_links map[linkInfo]*link // *link is nil if connection in progress
	// TODO timeout (to remove from switch), read from config.ReadTimeout
}

// linkInfo is used as a map key
type linkInfo struct {
	linkType string // Type of link, e.g. TCP, AWDL
	local    string // Local name or address
	remote   string // Remote name or address
}

type link struct {
	lname    string
	links    *links
	conn     *linkConn
	options  linkOptions
	info     linkInfo
	incoming bool
	force    bool
}

type linkOptions struct {
	pinnedEd25519Keys map[keyArray]struct{}
}

type Listener struct {
	net.Listener
	closed chan struct{}
}

func (l *Listener) Close() error {
	err := l.Listener.Close()
	<-l.closed
	return err
}

func (l *links) init(c *Core) error {
	l.core = c
	l.tcp = l.newLinkTCP()
	l.tls = l.newLinkTLS(l.tcp)
	l.unix = l.newLinkUNIX()
	l._links = make(map[linkInfo]*link)

	var listeners []ListenAddress
	phony.Block(c, func() {
		listeners = make([]ListenAddress, 0, len(c.config._listeners))
		for listener := range c.config._listeners {
			listeners = append(listeners, listener)
		}
	})

	return nil
}

func (l *links) shutdown() error {
	phony.Block(l.tcp, func() {
		for l := range l.tcp._listeners {
			l.Close()
		}
	})
	phony.Block(l.tls, func() {
		for l := range l.tls._listeners {
			l.Close()
		}
	})
	phony.Block(l.unix, func() {
		for l := range l.unix._listeners {
			l.Close()
		}
	})
	return nil
}

func (l *links) isConnectedTo(info linkInfo) bool {
	var isConnected bool
	phony.Block(l, func() {
		_, isConnected = l._links[info]
	})
	return isConnected
}

func (l *links) call(u *url.URL, sintf string) error {
	info := linkInfoFor(u.Scheme, sintf, u.Host)
	if l.isConnectedTo(info) {
		return fmt.Errorf("already connected to this node")
	}
	tcpOpts := tcpOptions{
		linkOptions: linkOptions{
			pinnedEd25519Keys: map[keyArray]struct{}{},
		},
	}
	for _, pubkey := range u.Query()["key"] {
		if sigPub, err := hex.DecodeString(pubkey); err == nil {
			var sigPubKey keyArray
			copy(sigPubKey[:], sigPub)
			tcpOpts.pinnedEd25519Keys[sigPubKey] = struct{}{}
		}
	}
	switch info.linkType {
	case "tcp":
		go func() {
			if err := l.tcp.dial(u, tcpOpts, sintf); err != nil {
				l.core.log.Warnf("Failed to dial TCP %s: %s\n", u.Host, err)
			}
		}()

		/*
			case "socks":
				tcpOpts.socksProxyAddr = u.Host
				if u.User != nil {
					tcpOpts.socksProxyAuth = &proxy.Auth{}
					tcpOpts.socksProxyAuth.User = u.User.Username()
					tcpOpts.socksProxyAuth.Password, _ = u.User.Password()
				}
				tcpOpts.upgrade = l.tcp.tls.forDialer // TODO make this configurable
				pathtokens := strings.Split(strings.Trim(u.Path, "/"), "/")
				go l.tcp.call(pathtokens[0], tcpOpts, sintf)
		*/

	case "tls":
		// SNI headers must contain hostnames and not IP addresses, so we must make sure
		// that we do not populate the SNI with an IP literal. We do this by splitting
		// the host-port combo from the query option and then seeing if it parses to an
		// IP address successfully or not.
		if sni := u.Query().Get("sni"); sni != "" {
			if net.ParseIP(sni) == nil {
				tcpOpts.tlsSNI = sni
			}
		}
		// If the SNI is not configured still because the above failed then we'll try
		// again but this time we'll use the host part of the peering URI instead.
		if tcpOpts.tlsSNI == "" {
			if host, _, err := net.SplitHostPort(u.Host); err == nil && net.ParseIP(host) == nil {
				tcpOpts.tlsSNI = host
			}
		}
		go func() {
			if err := l.tls.dial(u, tcpOpts, sintf); err != nil {
				l.core.log.Warnf("Failed to dial TLS %s: %s\n", u.Host, err)
			}
		}()

	case "unix":
		go func() {
			if err := l.unix.dial(u, tcpOpts.linkOptions, sintf); err != nil {
				l.core.log.Warnf("Failed to dial UNIX %s: %s\n", u.Host, err)
			}
		}()

	default:
		return errors.New("unknown call scheme: " + u.Scheme)
	}
	return nil
}

func (l *links) listen(u *url.URL, sintf string) (*Listener, error) {
	var listener *Listener
	var err error
	switch u.Scheme {
	case "tcp":
		listener, err = l.tcp.listen(u, sintf)
	case "tls":
		listener, err = l.tls.listen(u, sintf)
	case "unix":
		listener, err = l.unix.listen(u, sintf)
	default:
		return nil, fmt.Errorf("unrecognised scheme %q", u.Scheme)
	}
	return listener, err
}

func (l *links) create(conn net.Conn, name string, info linkInfo, incoming, force bool, options linkOptions) error {
	intf := link{
		conn: &linkConn{
			Conn: conn,
			up:   time.Now(),
		},
		lname:    name,
		links:    l,
		options:  options,
		info:     info,
		incoming: incoming,
		force:    force,
	}
	go func() {
		if err := intf.handler(); err != nil {
			l.core.log.Errorf("Link handler %s error (%s): %s", name, conn.RemoteAddr(), err)
		}
	}()
	return nil
}

func (intf *link) handler() error {
	defer intf.conn.Close()

	// Don't connect to this link more than once.
	if intf.links.isConnectedTo(intf.info) {
		return fmt.Errorf("already connected to this node")
	}

	// Mark the connection as in progress.
	phony.Block(intf.links, func() {
		intf.links._links[intf.info] = nil
	})

	// When we're done, clean up the connection entry.
	defer phony.Block(intf.links, func() {
		delete(intf.links._links, intf.info)
	})

	// TODO split some of this into shorter functions, so it's easier to read, and for the FIXME duplicate peer issue mentioned later
	meta := version_getBaseMetadata()
	meta.key = intf.links.core.public
	metaBytes := meta.encode()
	// TODO timeouts on send/recv (goroutine for send/recv, channel select w/ timer)
	var err error
	if !util.FuncTimeout(30*time.Second, func() {
		var n int
		n, err = intf.conn.Write(metaBytes)
		if err == nil && n != len(metaBytes) {
			err = errors.New("incomplete metadata send")
		}
	}) {
		return errors.New("timeout on metadata send")
	}
	if err != nil {
		return fmt.Errorf("write handshake: %w", err)
	}
	if !util.FuncTimeout(30*time.Second, func() {
		var n int
		n, err = io.ReadFull(intf.conn, metaBytes)
		if err == nil && n != len(metaBytes) {
			err = errors.New("incomplete metadata recv")
		}
	}) {
		return errors.New("timeout on metadata recv")
	}
	if err != nil {
		return fmt.Errorf("read handshake: %w", err)
	}
	meta = version_metadata{}
	base := version_getBaseMetadata()
	if !meta.decode(metaBytes) {
		return errors.New("failed to decode metadata")
	}
	if !meta.check() {
		var connectError string
		if intf.incoming {
			connectError = "Rejected incoming connection"
		} else {
			connectError = "Failed to connect"
		}
		intf.links.core.log.Debugf("%s: %s is incompatible version (local %s, remote %s)",
			connectError,
			intf.lname,
			fmt.Sprintf("%d.%d", base.ver, base.minorVer),
			fmt.Sprintf("%d.%d", meta.ver, meta.minorVer),
		)
		return errors.New("remote node is incompatible version")
	}
	// Check if the remote side matches the keys we expected. This is a bit of a weak
	// check - in future versions we really should check a signature or something like that.
	if pinned := intf.options.pinnedEd25519Keys; len(pinned) > 0 {
		var key keyArray
		copy(key[:], meta.key)
		if _, allowed := pinned[key]; !allowed {
			intf.links.core.log.Errorf("Failed to connect to node: %q sent ed25519 key that does not match pinned keys", intf.name())
			return fmt.Errorf("failed to connect: host sent ed25519 key that does not match pinned keys")
		}
	}
	// Check if we're authorized to connect to this key / IP
	allowed := intf.links.core.config._allowedPublicKeys
	isallowed := len(allowed) == 0
	for k := range allowed {
		if bytes.Equal(k[:], meta.key) {
			isallowed = true
			break
		}
	}
	if intf.incoming && !intf.force && !isallowed {
		intf.links.core.log.Warnf("%s connection from %s forbidden: AllowedEncryptionPublicKeys does not contain key %s",
			strings.ToUpper(intf.info.linkType), intf.info.remote, hex.EncodeToString(meta.key))
		intf.close()
		return fmt.Errorf("forbidden connection")
	}

	phony.Block(intf.links, func() {
		intf.links._links[intf.info] = intf
	})

	remoteAddr := net.IP(address.AddrForKey(meta.key)[:]).String()
	remoteStr := fmt.Sprintf("%s@%s", remoteAddr, intf.info.remote)
	localStr := intf.conn.LocalAddr()
	intf.links.core.log.Infof("Connected %s: %s, source %s",
		strings.ToUpper(intf.info.linkType), remoteStr, localStr)

	// TODO don't report an error if it's just a 'use of closed network connection'
	if err = intf.links.core.HandleConn(meta.key, intf.conn); err != nil && err != io.EOF {
		intf.links.core.log.Infof("Disconnected %s: %s, source %s; error: %s",
			strings.ToUpper(intf.info.linkType), remoteStr, localStr, err)
	} else {
		intf.links.core.log.Infof("Disconnected %s: %s, source %s",
			strings.ToUpper(intf.info.linkType), remoteStr, localStr)
	}

	return nil
}

func (intf *link) close() error {
	return intf.conn.Close()
}

func (intf *link) name() string {
	return intf.lname
}

func linkInfoFor(linkType, sintf, remote string) linkInfo {
	if h, _, err := net.SplitHostPort(remote); err == nil {
		remote = h
	}
	return linkInfo{
		linkType: linkType,
		local:    sintf,
		remote:   remote,
	}
}

type linkConn struct {
	// tx and rx are at the beginning of the struct to ensure 64-bit alignment
	// on 32-bit platforms, see https://pkg.go.dev/sync/atomic#pkg-note-BUG
	rx uint64
	tx uint64
	up time.Time
	net.Conn
}

func (c *linkConn) Read(p []byte) (n int, err error) {
	n, err = c.Conn.Read(p)
	atomic.AddUint64(&c.rx, uint64(n))
	return
}

func (c *linkConn) Write(p []byte) (n int, err error) {
	n, err = c.Conn.Write(p)
	atomic.AddUint64(&c.tx, uint64(n))
	return
}
