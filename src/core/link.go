package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Arceliar/phony"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"golang.org/x/crypto/blake2b"
)

type linkType int

const (
	linkTypePersistent linkType = iota // Statically configured
	linkTypeEphemeral                  // Multicast discovered
	linkTypeIncoming                   // Incoming connection
)

const defaultBackoffLimit = time.Second << 12 // 1h8m16s
const minimumBackoffLimit = time.Second * 30

type links struct {
	phony.Inbox
	core  *Core
	tcp   *linkTCP   // TCP interface support
	tls   *linkTLS   // TLS interface support
	unix  *linkUNIX  // UNIX interface support
	socks *linkSOCKS // SOCKS interface support
	quic  *linkQUIC  // QUIC interface support
	// _links can only be modified safely from within the links actor
	_links map[linkInfo]*link // *link is nil if connection in progress
}

type linkProtocol interface {
	dial(ctx context.Context, url *url.URL, info linkInfo, options linkOptions) (net.Conn, error)
	listen(ctx context.Context, url *url.URL, sintf string) (net.Listener, error)
}

// linkInfo is used as a map key
type linkInfo struct {
	uri   string // Peering URI in complete form
	sintf string // Peering source interface (i.e. from InterfacePeers)
}

// link tracks the state of a connection, either persistent or non-persistent
type link struct {
	ctx       context.Context    // Connection context
	cancel    context.CancelFunc // Stop future redial attempts (when peer removed)
	kick      chan struct{}      // Attempt to reconnect now, if backing off
	linkType  linkType           // Type of link, i.e. outbound/inbound, persistent/ephemeral
	linkProto string             // Protocol carrier of link, e.g. TCP, AWDL
	// The remaining fields can only be modified safely from within the links actor
	_conn    *linkConn // Connected link, if any, nil if not connected
	_err     error     // Last error on the connection, if any
	_errtime time.Time // Last time an error occurred
}

type linkOptions struct {
	pinnedEd25519Keys map[keyArray]struct{}
	priority          uint8
	tlsSNI            string
	password          []byte
	maxBackoff        time.Duration
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
	l.quic = l.newLinkQUIC()
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

type linkError string

func (e linkError) Error() string { return string(e) }

const ErrLinkAlreadyConfigured = linkError("peer is already configured")
const ErrLinkNotConfigured = linkError("peer is not configured")
const ErrLinkPriorityInvalid = linkError("priority value is invalid")
const ErrLinkPinnedKeyInvalid = linkError("pinned public key is invalid")
const ErrLinkPasswordInvalid = linkError("password is invalid")
const ErrLinkUnrecognisedSchema = linkError("link schema unknown")
const ErrLinkMaxBackoffInvalid = linkError("max backoff duration invalid")

func (l *links) add(u *url.URL, sintf string, linkType linkType) error {
	var retErr error
	phony.Block(l, func() {
		// Generate the link info and see whether we think we already
		// have an open peering to this peer.
		lu := urlForLinkInfo(*u)
		info := linkInfo{
			uri:   lu.String(),
			sintf: sintf,
		}

		// Collect together the link options, these are global options
		// that are not specific to any given protocol.
		options := linkOptions{
			maxBackoff: defaultBackoffLimit,
		}
		for _, pubkey := range u.Query()["key"] {
			sigPub, err := hex.DecodeString(pubkey)
			if err != nil {
				retErr = ErrLinkPinnedKeyInvalid
				return
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
				retErr = ErrLinkPriorityInvalid
				return
			}
			options.priority = uint8(pi)
		}
		if p := u.Query().Get("password"); p != "" {
			if len(p) > blake2b.Size {
				retErr = ErrLinkPasswordInvalid
				return
			}
			options.password = []byte(p)
		}
		if p := u.Query().Get("maxbackoff"); p != "" {
			d, err := time.ParseDuration(p)
			if err != nil || d < minimumBackoffLimit {
				retErr = ErrLinkMaxBackoffInvalid
				return
			}
			options.maxBackoff = d
		}
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

		// If we think we're already connected to this peer, load up
		// the existing peer state. Try to kick the peer if possible,
		// which will cause an immediate connection attempt if it is
		// backing off for some reason.
		state, ok := l._links[info]
		if ok && state != nil {
			select {
			case state.kick <- struct{}{}:
			default:
			}
			retErr = ErrLinkAlreadyConfigured
			return
		}

		// Create the link entry. This will contain the connection
		// in progress (if any), any error details and a context that
		// lets the link be cancelled later.
		state = &link{
			linkType:  linkType,
			linkProto: strings.ToUpper(u.Scheme),
			kick:      make(chan struct{}),
		}
		state.ctx, state.cancel = context.WithCancel(l.core.ctx)

		// Store the state of the link so that it can be queried later.
		l._links[info] = state

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
			if backoff < 32 {
				backoff++
			}
			duration := time.Second << backoff
			if duration > options.maxBackoff {
				duration = options.maxBackoff
			}
			select {
			case <-state.kick:
				return true
			case <-state.ctx.Done():
				return false
			case <-l.core.ctx.Done():
				return false
			case <-time.After(duration):
				return true
			}
		}

		// resetBackoff is called by the connection handler when the
		// handshake has successfully completed.
		resetBackoff := func() {
			backoff = 0
		}

		// The goroutine is responsible for attempting the connection
		// and then running the handler. If the connection is persistent
		// then the loop will run endlessly, using backoffs as needed.
		// Otherwise the loop will end, cleaning up the link entry.
		go func() {
			defer phony.Block(l, func() {
				if l._links[info] == state {
					delete(l._links, info)
				}
			})

			// This loop will run each and every time we want to attempt
			// a connection to this peer.
			// TODO get rid of this loop, this is *exactly* what time.AfterFunc is for, we should just send a signal to the links actor to kick off a goroutine as needed
			for {
				select {
				case <-state.ctx.Done():
					// The peering context has been cancelled, so don't try
					// to dial again.
					return
				default:
				}

				conn, err := l.connect(state.ctx, u, info, options)
				if err != nil || conn == nil {
					if err == nil && conn == nil {
						l.core.log.Warnf("Link %q reached inconsistent error state", u.String())
					}
					if linkType == linkTypePersistent {
						// If the link is a persistent configured peering,
						// store information about the connection error so
						// that we can report it through the admin socket.
						phony.Block(l, func() {
							state._conn = nil
							state._err = err
							state._errtime = time.Now()
						})

						// Back off for a bit. If true is returned here, we
						// can continue onto the next loop iteration to try
						// the next connection.
						if backoffNow() {
							continue
						}
						return
					}
					// Ephemeral and incoming connections don't remain
					// after a connection failure, so exit out of the
					// loop and clean up the link entry.
					break
				}

				// The linkConn wrapper allows us to track the number of
				// bytes written to and read from this connection without
				// the help of ironwood.
				lc := &linkConn{
					Conn: conn,
					up:   time.Now(),
				}

				// Update the link state with our newly wrapped connection.
				// Clear the error state.
				var doRet bool
				phony.Block(l, func() {
					if state._conn != nil {
						// If a peering has come up in this time, abort this one.
						doRet = true
					}
					state._conn = lc
				})
				if doRet {
					return
				}

				// Give the connection to the handler. The handler will block
				// for the lifetime of the connection.
				if err = l.handler(linkType, options, lc, resetBackoff); err != nil && err != io.EOF {
					l.core.log.Debugf("Link %s error: %s\n", info.uri, err)
				}

				// The handler has stopped running so the connection is dead,
				// try to close the underlying socket just in case and then
				// update the link state.
				_ = lc.Close()
				phony.Block(l, func() {
					state._conn = nil
					if state._err = err; state._err != nil {
						state._errtime = time.Now()
					}
				})

				// If the link is persistently configured, back off if needed
				// and then try reconnecting. Otherwise, exit out.
				if linkType == linkTypePersistent {
					if backoffNow() {
						continue
					}
					return
				}
			}
		}()
	})
	return retErr
}

func (l *links) remove(u *url.URL, sintf string, linkType linkType) error {
	var retErr error
	phony.Block(l, func() {
		// Generate the link info and see whether we think we already
		// have an open peering to this peer.
		lu := urlForLinkInfo(*u)
		info := linkInfo{
			uri:   lu.String(),
			sintf: sintf,
		}

		// If this peer is already configured then we will close the
		// connection and stop it from retrying.
		state, ok := l._links[info]
		if ok && state != nil {
			state.cancel()
			if conn := state._conn; conn != nil {
				retErr = conn.Close()
			}
			return
		}

		retErr = ErrLinkNotConfigured
	})
	return retErr
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
	case "quic":
		protocol = l.quic
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

	var options linkOptions
	if p := u.Query().Get("priority"); p != "" {
		pi, err := strconv.ParseUint(p, 10, 8)
		if err != nil {
			return nil, ErrLinkPriorityInvalid
		}
		options.priority = uint8(pi)
	}
	if p := u.Query().Get("password"); p != "" {
		if len(p) > blake2b.Size {
			return nil, ErrLinkPasswordInvalid
		}
		options.password = []byte(p)
	}

	go func() {
		l.core.log.Infof("%s listener started on %s", strings.ToUpper(u.Scheme), listener.Addr())
		defer l.core.log.Infof("%s listener stopped on %s", strings.ToUpper(u.Scheme), listener.Addr())
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()

				// In order to populate a somewhat sane looking connection
				// URI in the admin socket, we need to replace the host in
				// the listener URL with the remote address.
				pu := *u
				pu.Host = conn.RemoteAddr().String()
				lu := urlForLinkInfo(pu)
				info := linkInfo{
					uri:   lu.String(),
					sintf: sintf,
				}

				// If there's an existing link state for this link, get it.
				// If this node is already connected to us, just drop the
				// connection. This prevents duplicate peerings.
				var lc *linkConn
				var state *link
				phony.Block(l, func() {
					var ok bool
					state, ok = l._links[info]
					if !ok || state == nil {
						state = &link{
							linkType:  linkTypeIncoming,
							linkProto: strings.ToUpper(u.Scheme),
							kick:      make(chan struct{}),
						}
					}
					if state._conn != nil {
						// If a connection has come up in this time, abort
						// this one.
						return
					}

					// The linkConn wrapper allows us to track the number of
					// bytes written to and read from this connection without
					// the help of ironwood.
					lc = &linkConn{
						Conn: conn,
						up:   time.Now(),
					}

					// Update the link state with our newly wrapped connection.
					// Clear the error state.
					state._conn = lc
					state._err = nil
					state._errtime = time.Time{}

					// Store the state of the link so that it can be queried later.
					l._links[info] = state
				})
				if lc == nil {
					return
				}

				// Give the connection to the handler. The handler will block
				// for the lifetime of the connection.
				if err = l.handler(linkTypeIncoming, options, lc, nil); err != nil && err != io.EOF {
					l.core.log.Debugf("Link %s error: %s\n", u.Host, err)
				}

				// The handler has stopped running so the connection is dead,
				// try to close the underlying socket just in case and then
				// drop the link state.
				_ = lc.Close()
				phony.Block(l, func() {
					if l._links[info] == state {
						delete(l._links, info)
					}
				})
			}(conn)
		}
	}()
	return li, nil
}

func (l *links) connect(ctx context.Context, u *url.URL, info linkInfo, options linkOptions) (net.Conn, error) {
	var dialer linkProtocol
	switch strings.ToLower(u.Scheme) {
	case "tcp":
		dialer = l.tcp
	case "tls":
		dialer = l.tls
	case "socks", "sockstls":
		dialer = l.socks
	case "unix":
		dialer = l.unix
	case "quic":
		dialer = l.quic
	default:
		return nil, ErrLinkUnrecognisedSchema
	}
	return dialer.dial(ctx, u, info, options)
}

func (l *links) handler(linkType linkType, options linkOptions, conn net.Conn, success func()) error {
	meta := version_getBaseMetadata()
	meta.publicKey = l.core.public
	meta.priority = options.priority
	metaBytes, err := meta.encode(l.core.secret, options.password)
	if err != nil {
		return fmt.Errorf("failed to generate handshake: %w", err)
	}
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
	meta = version_metadata{}
	base := version_getBaseMetadata()
	if err := meta.decode(conn, options.password); err != nil {
		_ = conn.Close()
		return err
	}
	if !meta.check() {
		return fmt.Errorf("remote node incompatible version (local %s, remote %s)",
			fmt.Sprintf("%d.%d", base.majorVer, base.minorVer),
			fmt.Sprintf("%d.%d", meta.majorVer, meta.minorVer),
		)
	}
	if err = conn.SetDeadline(time.Time{}); err != nil {
		return fmt.Errorf("failed to clear handshake deadline: %w", err)
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
	if linkType == linkTypeIncoming && !isallowed {
		return fmt.Errorf("node public key %q is not in AllowedPublicKeys", hex.EncodeToString(meta.publicKey))
	}

	dir := "outbound"
	if linkType == linkTypeIncoming {
		dir = "inbound"
	}
	remoteAddr := net.IP(address.AddrForKey(meta.publicKey)[:]).String()
	remoteStr := fmt.Sprintf("%s@%s", remoteAddr, conn.RemoteAddr())
	localStr := conn.LocalAddr()
	priority := options.priority
	if meta.priority > priority {
		priority = meta.priority
	}
	l.core.log.Infof("Connected %s: %s, source %s",
		dir, remoteStr, localStr)
	if success != nil {
		success()
	}

	err = l.core.HandleConn(meta.publicKey, conn, priority)
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
