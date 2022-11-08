package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Arceliar/phony"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type links struct {
	phony.Inbox
	core   *Core
	tcp    *linkTCP           // TCP interface support
	tls    *linkTLS           // TLS interface support
	unix   *linkUNIX          // UNIX interface support
	socks  *linkSOCKS         // SOCKS interface support
	_links map[linkInfo]*link // *link is nil if connection in progress
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
	priority          uint8
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
	l.socks = l.newLinkSOCKS()
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

func (l *links) shutdown() {
	phony.Block(l.tcp, func() {
		for l := range l.tcp._listeners {
			_ = l.Close()
		}
	})
	phony.Block(l.tls, func() {
		for l := range l.tls._listeners {
			_ = l.Close()
		}
	})
	phony.Block(l.unix, func() {
		for l := range l.unix._listeners {
			_ = l.Close()
		}
	})
}

func (l *links) isConnectedTo(info linkInfo) bool {
	var isConnected bool
	phony.Block(l, func() {
		_, isConnected = l._links[info]
	})
	return isConnected
}

func (l *links) call(u *url.URL, sintf string) (linkInfo, error) {
	info := linkInfoFor(u.Scheme, sintf, u.Host)
	if l.isConnectedTo(info) {
		return info, nil
	}
	options := linkOptions{
		pinnedEd25519Keys: map[keyArray]struct{}{},
	}
	for _, pubkey := range u.Query()["key"] {
		sigPub, err := hex.DecodeString(pubkey)
		if err != nil {
			return info, fmt.Errorf("pinned key contains invalid hex characters")
		}
		var sigPubKey keyArray
		copy(sigPubKey[:], sigPub)
		options.pinnedEd25519Keys[sigPubKey] = struct{}{}
	}
	if p := u.Query().Get("priority"); p != "" {
		pi, err := strconv.ParseUint(p, 10, 8)
		if err != nil {
			return info, fmt.Errorf("priority invalid: %w", err)
		}
		options.priority = uint8(pi)
	}
	switch info.linkType {
	case "tcp":
		go func() {
			if err := l.tcp.dial(u, options, sintf); err != nil && err != io.EOF {
				l.core.log.Warnf("Failed to dial TCP %s: %s\n", u.Host, err)
			}
		}()

	case "socks":
		go func() {
			if err := l.socks.dial(u, options); err != nil && err != io.EOF {
				l.core.log.Warnf("Failed to dial SOCKS %s: %s\n", u.Host, err)
			}
		}()

	case "tls":
		// SNI headers must contain hostnames and not IP addresses, so we must make sure
		// that we do not populate the SNI with an IP literal. We do this by splitting
		// the host-port combo from the query option and then seeing if it parses to an
		// IP address successfully or not.
		var tlsSNI string
		if sni := u.Query().Get("sni"); sni != "" {
			if net.ParseIP(sni) == nil {
				tlsSNI = sni
			}
		}
		// If the SNI is not configured still because the above failed then we'll try
		// again but this time we'll use the host part of the peering URI instead.
		if tlsSNI == "" {
			if host, _, err := net.SplitHostPort(u.Host); err == nil && net.ParseIP(host) == nil {
				tlsSNI = host
			}
		}
		go func() {
			if err := l.tls.dial(u, options, sintf, tlsSNI); err != nil && err != io.EOF {
				l.core.log.Warnf("Failed to dial TLS %s: %s\n", u.Host, err)
			}
		}()

	case "unix":
		go func() {
			if err := l.unix.dial(u, options, sintf); err != nil && err != io.EOF {
				l.core.log.Warnf("Failed to dial UNIX %s: %s\n", u.Host, err)
			}
		}()

	default:
		return info, errors.New("unknown call scheme: " + u.Scheme)
	}
	return info, nil
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
	defer intf.conn.Close() // nolint:errcheck

	// Don't connect to this link more than once.
	if intf.links.isConnectedTo(intf.info) {
		return nil
	}

	// Mark the connection as in progress.
	phony.Block(intf.links, func() {
		intf.links._links[intf.info] = nil
	})

	// When we're done, clean up the connection entry.
	defer phony.Block(intf.links, func() {
		delete(intf.links._links, intf.info)
	})

	meta := version_getBaseMetadata()
	meta.key = intf.links.core.public
	metaBytes := meta.encode()
	if err := intf.conn.SetDeadline(time.Now().Add(time.Second * 6)); err != nil {
		return fmt.Errorf("failed to set handshake deadline: %w", err)
	}
	n, err := intf.conn.Write(metaBytes)
	switch {
	case err != nil:
		return fmt.Errorf("write handshake: %w", err)
	case err == nil && n != len(metaBytes):
		return fmt.Errorf("incomplete handshake send")
	}
	if _, err = io.ReadFull(intf.conn, metaBytes); err != nil {
		return fmt.Errorf("read handshake: %w", err)
	}
	if err = intf.conn.SetDeadline(time.Time{}); err != nil {
		return fmt.Errorf("failed to clear handshake deadline: %w", err)
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
			return fmt.Errorf("node public key that does not match pinned keys")
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
		_ = intf.close()
		return fmt.Errorf("node public key %q is not in AllowedPublicKeys", hex.EncodeToString(meta.key))
	}

	phony.Block(intf.links, func() {
		intf.links._links[intf.info] = intf
	})

	dir := "outbound"
	if intf.incoming {
		dir = "inbound"
	}
	remoteAddr := net.IP(address.AddrForKey(meta.key)[:]).String()
	remoteStr := fmt.Sprintf("%s@%s", remoteAddr, intf.info.remote)
	localStr := intf.conn.LocalAddr()
	intf.links.core.log.Infof("Connected %s %s: %s, source %s",
		dir, strings.ToUpper(intf.info.linkType), remoteStr, localStr)

	err = intf.links.core.HandleConn(meta.key, intf.conn, intf.options.priority)
	switch err {
	case io.EOF, net.ErrClosed, nil:
		intf.links.core.log.Infof("Disconnected %s %s: %s, source %s",
			dir, strings.ToUpper(intf.info.linkType), remoteStr, localStr)
	default:
		intf.links.core.log.Infof("Disconnected %s %s: %s, source %s; error: %s",
			dir, strings.ToUpper(intf.info.linkType), remoteStr, localStr, err)
	}
	return nil
}

func (intf *link) close() error {
	return intf.conn.Close()
}

func linkInfoFor(linkType, sintf, remote string) linkInfo {
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

func linkOptionsForListener(u *url.URL) (l linkOptions) {
	if p := u.Query().Get("priority"); p != "" {
		if pi, err := strconv.ParseUint(p, 10, 8); err == nil {
			l.priority = uint8(pi)
		}
	}
	return
}
