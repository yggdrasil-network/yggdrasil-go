package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Arceliar/phony"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
)

type linkType int

const (
	linkTypePersistent linkType = iota // Statically configured
	linkTypeEphemeral                  // Multicast discovered
	linkTypeIncoming                   // Incoming connection
)

type links struct {
	core         *Core
	tcp          *linkTCP           // TCP interface support
	tls          *linkTLS           // TLS interface support
	unix         *linkUNIX          // UNIX interface support
	socks        *linkSOCKS         // SOCKS interface support
	sync.RWMutex                    // Protects the below
	_links       map[linkInfo]*link // *link is nil if connection in progress
}

type linkProtocol interface {
	dial(url *url.URL, info linkInfo, options linkOptions) (net.Conn, error)
	listen(ctx context.Context, url *url.URL, sintf string) (net.Listener, error)
}

// linkInfo is used as a map key
type linkInfo struct {
	uri      string   // Peering URI in complete form
	sintf    string   // Peering source interface (i.e. from InterfacePeers)
	linkType linkType // Type of link, i.e. outbound/inbound, persistent/ephemeral
}

// link tracks the state of a connection, either persistent or non-persistent
type link struct {
	phony.Inbox
	ctx       context.Context    //
	cancel    context.CancelFunc //
	kick      chan struct{}      // Attempt to reconnect now, if backing off
	info      linkInfo           //
	linkProto string             // Protocol carrier of link, e.g. TCP, AWDL
	_conn     *linkConn          // Connected link, if any, nil if not connected
	_err      error              // Last error on the connection, if any
	_errtime  time.Time          // Last time an error occured

}

type linkOptions struct {
	pinnedEd25519Keys map[keyArray]struct{}
	priority          uint8
	tlsSNI            string
}

type Listener struct {
	listener net.Listener
	ctx      context.Context
	Cancel   context.CancelFunc
}

func (l *Listener) Addr() net.Addr {
	return l.listener.Addr()
}

func (l *Listener) Close() error {
	l.Cancel()
	err := l.listener.Close()
	<-l.ctx.Done()
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
	l.RLock()
	link, ok := l._links[info]
	l.RUnlock()
	if !ok {
		return false
	}
	return link._conn != nil
}

type linkError string

func (e linkError) Error() string { return string(e) }

const ErrLinkAlreadyConfigured = linkError("peer is already configured")
const ErrLinkPriorityInvalid = linkError("priority value is invalid")
const ErrLinkPinnedKeyInvalid = linkError("pinned public key is invalid")
const ErrLinkUnrecognisedSchema = linkError("link schema unknown")

func (l *links) add(u *url.URL, sintf string, linkType linkType) error {
	// Generate the link info and see whether we think we already
	// have an open peering to this peer.
	lu := urlForLinkInfo(*u)
	info := linkInfo{
		uri:      lu.String(),
		sintf:    sintf,
		linkType: linkType,
	}
	l.RLock()
	state, ok := l._links[info]
	l.RUnlock()
	if ok && state != nil {
		select {
		case state.kick <- struct{}{}:
		default:
		}
		return ErrLinkAlreadyConfigured
	}

	// Create the link entry. This will contain the connection
	// in progress (if any), any error details and a context that
	// lets the link be cancelled later.
	ctx, cancel := context.WithCancel(l.core.ctx)
	state = &link{
		info:      info,
		linkProto: strings.ToUpper(u.Scheme),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Collect together the link options, these are global options
	// that are not specific to any given protocol.
	var options linkOptions
	for _, pubkey := range u.Query()["key"] {
		sigPub, err := hex.DecodeString(pubkey)
		if err != nil {
			return ErrLinkPinnedKeyInvalid
		}
		var sigPubKey keyArray
		copy(sigPubKey[:], sigPub)
		if options.pinnedEd25519Keys == nil {
			options.pinnedEd25519Keys = map[keyArray]struct{}{}
		}
		options.pinnedEd25519Keys[sigPubKey] = struct{}{}
	}
	if p := u.Query().Get("priority"); p != "" {
		pi, err := strconv.ParseUint(p, 10, 8)
		if err != nil {
			return ErrLinkPriorityInvalid
		}
		options.priority = uint8(pi)
	}

	// Store the state of the link, try to connect and then run
	// the handler.
	l.Lock()
	l._links[info] = state
	l.Unlock()

	// Track how many consecutive connection failures we have had,
	// as we will back off exponentially rather than hammering the
	// remote node endlessly.
	var backoff int

	// backoffNow is called when there's a connection error. It
	// will wait for the specified amount of time and then return
	// true, unless the peering context was cancelled (due to a
	// peer removal most likely), in which case it returns false.
	// The caller should check the return value to decide whether
	// or not to give up trying.
	backoffNow := func() bool {
		backoff++
		duration := time.Second * time.Duration(math.Exp2(float64(backoff)))
		select {
		case <-time.After(duration):
			return true
		case <-state.kick:
			return true
		case <-ctx.Done():
			return false
		}
	}

	// The goroutine is responsible for attempting the connection
	// and then running the handler. If the connection is persistent
	// then the loop will run endlessly, using backoffs as needed.
	// Otherwise the loop will end, cleaning up the link entry.
	go func() {
		defer func() {
			l.Lock()
			defer l.Unlock()
			delete(l._links, info)
		}()
		for {
			conn, err := l.connect(u, info, options)
			if err != nil {
				if linkType == linkTypePersistent {
					phony.Block(state, func() {
						state._err = err
						state._errtime = time.Now()
					})
					if backoffNow() {
						continue
					} else {
						return
					}
				} else {
					break
				}
			}
			lc := &linkConn{
				Conn: conn,
				up:   time.Now(),
			}
			phony.Block(state, func() {
				state._conn = lc
				state._err = nil
				state._errtime = time.Time{}
			})
			if err = l.handler(&info, options, lc); err != nil && err != io.EOF {
				l.core.log.Debugf("Link %s error: %s\n", info.uri, err)
			} else {
				backoff = 0
			}
			_ = conn.Close()
			phony.Block(state, func() {
				state._conn = nil
				if state._err = err; state._err != nil {
					state._errtime = time.Now()
				}
			})
			if linkType == linkTypePersistent {
				if backoffNow() {
					continue
				} else {
					return
				}
			} else {
				break
			}
		}
	}()
	return nil
}

func (l *links) listen(u *url.URL, sintf string) (*Listener, error) {
	ctx, cancel := context.WithCancel(l.core.ctx)
	var protocol linkProtocol
	switch strings.ToLower(u.Scheme) {
	case "tcp":
		protocol = l.tcp
	case "tls":
		protocol = l.tls
	case "unix":
		protocol = l.unix
	default:
		cancel()
		return nil, ErrLinkUnrecognisedSchema
	}
	listener, err := protocol.listen(ctx, u, sintf)
	if err != nil {
		cancel()
		return nil, err
	}
	li := &Listener{
		listener: listener,
		ctx:      ctx,
		Cancel:   cancel,
	}
	go func() {
		l.core.log.Printf("%s listener started on %s", strings.ToUpper(u.Scheme), listener.Addr())
		defer l.core.log.Printf("%s listener stopped on %s", strings.ToUpper(u.Scheme), listener.Addr())
		for {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			pu := *u
			pu.Host = conn.RemoteAddr().String()
			lu := urlForLinkInfo(pu)
			info := linkInfo{
				uri:      lu.String(),
				sintf:    sintf,
				linkType: linkTypeEphemeral, // TODO: should be incoming
			}
			if l.isConnectedTo(info) {
				_ = conn.Close()
				continue
			}
			l.RLock()
			state, ok := l._links[info]
			l.RUnlock()
			if !ok || state == nil {
				state = &link{
					info: info,
				}
			}
			lc := &linkConn{
				Conn: conn,
				up:   time.Now(),
			}
			var options linkOptions
			phony.Block(state, func() {
				state._conn = lc
				state._err = nil
				state.linkProto = strings.ToUpper(u.Scheme)
			})
			l.Lock()
			l._links[info] = state
			l.Unlock()
			if err = l.handler(&info, options, lc); err != nil && err != io.EOF {
				l.core.log.Debugf("Link %s error: %s\n", u.Host, err)
			}
			phony.Block(state, func() {
				state._conn = nil
				if state._err = err; state._err != nil {
					state._errtime = time.Now()
				}
			})
			l.Lock()
			delete(l._links, info)
			l.Unlock()
		}
	}()
	return li, nil
}

func (l *links) connect(u *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	var dialer linkProtocol
	switch strings.ToLower(u.Scheme) {
	case "tcp":
		dialer = l.tcp
	case "tls":
		// SNI headers must contain hostnames and not IP addresses, so we must make sure
		// that we do not populate the SNI with an IP literal. We do this by splitting
		// the host-port combo from the query option and then seeing if it parses to an
		// IP address successfully or not.
		if sni := u.Query().Get("sni"); sni != "" {
			if net.ParseIP(sni) == nil {
				options.tlsSNI = sni
			}
		}
		// If the SNI is not configured still because the above failed then we'll try
		// again but this time we'll use the host part of the peering URI instead.
		if options.tlsSNI == "" {
			if host, _, err := net.SplitHostPort(u.Host); err == nil && net.ParseIP(host) == nil {
				options.tlsSNI = host
			}
		}
		dialer = l.tls
	case "socks":
		dialer = l.socks
	case "unix":
		dialer = l.unix
	default:
		return nil, ErrLinkUnrecognisedSchema
	}
	return dialer.dial(u, info, options)
}

func (l *links) handler(info *linkInfo, options linkOptions, conn net.Conn) error {
	meta := version_getBaseMetadata()
	meta.publicKey = l.core.public
	metaBytes := meta.encode()
	if err := conn.SetDeadline(time.Now().Add(time.Second * 6)); err != nil {
		return fmt.Errorf("failed to set handshake deadline: %w", err)
	}
	n, err := conn.Write(metaBytes)
	switch {
	case err != nil:
		return fmt.Errorf("write handshake: %w", err)
	case err == nil && n != len(metaBytes):
		return fmt.Errorf("incomplete handshake send")
	}
	if _, err = io.ReadFull(conn, metaBytes); err != nil {
		return fmt.Errorf("read handshake: %w", err)
	}
	if err = conn.SetDeadline(time.Time{}); err != nil {
		return fmt.Errorf("failed to clear handshake deadline: %w", err)
	}
	meta = version_metadata{}
	base := version_getBaseMetadata()
	if !meta.decode(metaBytes) {
		return errors.New("failed to decode metadata")
	}
	if !meta.check() {
		return fmt.Errorf("remote node incompatible version (local %s, remote %s)",
			fmt.Sprintf("%d.%d", base.majorVer, base.minorVer),
			fmt.Sprintf("%d.%d", meta.majorVer, meta.minorVer),
		)
	}
	// Check if the remote side matches the keys we expected. This is a bit of a weak
	// check - in future versions we really should check a signature or something like that.
	if pinned := options.pinnedEd25519Keys; len(pinned) > 0 {
		var key keyArray
		copy(key[:], meta.publicKey)
		if _, allowed := pinned[key]; !allowed {
			return fmt.Errorf("node public key that does not match pinned keys")
		}
	}
	// Check if we're authorized to connect to this key / IP
	var allowed map[[32]byte]struct{}
	phony.Block(l.core, func() {
		allowed = l.core.config._allowedPublicKeys
	})
	isallowed := len(allowed) == 0
	for k := range allowed {
		if bytes.Equal(k[:], meta.publicKey) {
			isallowed = true
			break
		}
	}
	if info.linkType == linkTypeIncoming && !isallowed {
		return fmt.Errorf("node public key %q is not in AllowedPublicKeys", hex.EncodeToString(meta.publicKey))
	}

	dir := "outbound"
	if info.linkType == linkTypeIncoming {
		dir = "inbound"
	}
	remoteAddr := net.IP(address.AddrForKey(meta.publicKey)[:]).String()
	remoteStr := fmt.Sprintf("%s@%s", remoteAddr, conn.RemoteAddr())
	localStr := conn.LocalAddr()
	l.core.log.Infof("Connected %s: %s, source %s",
		dir, remoteStr, localStr)

	err = l.core.HandleConn(meta.publicKey, conn, options.priority)
	switch err {
	case io.EOF, net.ErrClosed, nil:
		l.core.log.Infof("Disconnected %s: %s, source %s",
			dir, remoteStr, localStr)
	default:
		l.core.log.Infof("Disconnected %s: %s, source %s; error: %s",
			dir, remoteStr, localStr, err)
	}
	return nil
}

func urlForLinkInfo(u url.URL) url.URL {
	u.RawQuery = ""
	if host, _, err := net.SplitHostPort(u.Host); err == nil {
		if addr, err := netip.ParseAddr(host); err == nil {
			// For peers that look like multicast peers (i.e.
			// link-local addresses), we will ignore the port number,
			// otherwise we might open multiple connections to them.
			if addr.IsLinkLocalUnicast() {
				u.Host = fmt.Sprintf("[%s]", addr.String())
			}
		}
	}
	return u
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
