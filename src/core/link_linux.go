//go:build linux
// +build linux

package core

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"

	"github.com/Arceliar/phony"
	//"github.com/Arceliar/phony" // TODO? use instead of mutexes
)

type links struct {
	phony.Inbox
	core   *Core
	tcp    *linkTCP           // TCP interface support
	tls    *linkTLS           // TLS interface support
	unix   *linkUNIX          // UNIX interface support
	socks  *linkSOCKS         // SOCKS interface support
	sctp   *linkSCTP          // SCTP interface support
	mpath  *linkMPATH         // Multipath interface support
	_links map[linkInfo]*link // *link is nil if connection in progress
	// TODO timeout (to remove from switch), read from config.ReadTimeout
}

func (l *links) init(c *Core) error {
	l.core = c
	l.tcp = l.newLinkTCP()
	l.tls = l.newLinkTLS(l.tcp)
	l.unix = l.newLinkUNIX()
	l.socks = l.newLinkSOCKS()
	l.sctp = l.newLinkSCTP()
	l.mpath = l.newLinkMPATH()

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

func (l *links) call(u *url.URL, sintf string, errch chan<- error) (info linkInfo, err error) {
	info = linkInfoFor(u.Scheme, sintf, u.Host)
	if l.isConnectedTo(info) {
		if errch != nil {
			close(errch) // already connected, no error
		}
		return info, nil
	}
	options := linkOptions{
		pinnedEd25519Keys: map[keyArray]struct{}{},
	}
	for _, pubkey := range u.Query()["key"] {
		sigPub, err := hex.DecodeString(pubkey)
		if err != nil {
			if errch != nil {
				close(errch)
			}
			return info, fmt.Errorf("pinned key contains invalid hex characters")
		}
		var sigPubKey keyArray
		copy(sigPubKey[:], sigPub)
		options.pinnedEd25519Keys[sigPubKey] = struct{}{}
	}
	if p := u.Query().Get("priority"); p != "" {
		pi, err := strconv.ParseUint(p, 10, 8)
		if err != nil {
			if errch != nil {
				close(errch)
			}
			return info, fmt.Errorf("priority invalid: %w", err)
		}
		options.priority = uint8(pi)
	}
	switch info.linkType {
	case "tcp":
		go func() {
			if errch != nil {
				defer close(errch)
			}
			if err := l.tcp.dial(u, options, sintf); err != nil && err != io.EOF {
				l.core.log.Warnf("Failed to dial TCP %s: %s\n", u.Host, err)
				if errch != nil {
					errch <- err
				}
			}
		}()

	case "socks":
		go func() {
			if errch != nil {
				defer close(errch)
			}
			if err := l.socks.dial(u, options); err != nil && err != io.EOF {
				l.core.log.Warnf("Failed to dial SOCKS %s: %s\n", u.Host, err)
				if errch != nil {
					errch <- err
				}
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
			if errch != nil {
				defer close(errch)
			}
			if err := l.tls.dial(u, options, sintf, tlsSNI); err != nil && err != io.EOF {
				l.core.log.Warnf("Failed to dial TLS %s: %s\n", u.Host, err)
				if errch != nil {
					errch <- err
				}
			}
		}()

	case "unix":
		go func() {
			if errch != nil {
				defer close(errch)
			}
			if err := l.unix.dial(u, options, sintf); err != nil && err != io.EOF {
				l.core.log.Warnf("Failed to dial UNIX %s: %s\n", u.Host, err)
				if errch != nil {
					errch <- err
				}
			}
		}()
	case "sctp":
		go func() {
			if errch != nil {
				defer close(errch)
			}
			if err := l.sctp.dial(u, options, sintf); err != nil && err != io.EOF {
				l.core.log.Warnf("Failed to dial SCTP %s: %s\n", u.Host, err)
				if errch != nil {
					errch <- err
				}
			}
		}()
	case "mpath":
		go func() {
			if errch != nil {
				defer close(errch)
			}
			if err := l.mpath.dial(u, options, sintf); err != nil && err != io.EOF {
				l.core.log.Warnf("Failed to dial MPATH %s: %s\n", u.Host, err)
				if errch != nil {
					errch <- err
				}
			}
		}()
	default:
		if errch != nil {
			close(errch)
		}
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
	case "sctp":
		listener, err = l.sctp.listen(u, sintf)
	case "mpath":
		listener, err = l.mpath.listen(u, sintf)
	default:
		return nil, fmt.Errorf("unrecognised scheme %q", u.Scheme)
	}
	return listener, err
}
