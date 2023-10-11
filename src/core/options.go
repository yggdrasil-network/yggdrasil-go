package core

import (
	"crypto/ed25519"
	"fmt"
	"net/url"
)

func (c *Core) _applyOption(opt SetupOption) (err error) {
	switch v := opt.(type) {
	case Peer:
		u, err := url.Parse(v.URI)
		if err != nil {
			return fmt.Errorf("unable to parse peering URI: %w", err)
		}
		return c.links.add(u, v.SourceInterface, linkTypePersistent)
	case ListenAddress:
		c.config._listeners[v] = struct{}{}
	case NodeInfo:
		c.config.nodeinfo = v
	case NodeInfoPrivacy:
		c.config.nodeinfoPrivacy = v
	case AllowedPublicKey:
		pk := [32]byte{}
		copy(pk[:], v)
		c.config._allowedPublicKeys[pk] = struct{}{}
	}
	return
}

type SetupOption interface {
	isSetupOption()
}

type ListenAddress string
type Peer struct {
	URI             string
	SourceInterface string
}
type NodeInfo map[string]interface{}
type NodeInfoPrivacy bool
type AllowedPublicKey ed25519.PublicKey

func (a ListenAddress) isSetupOption()    {}
func (a Peer) isSetupOption()             {}
func (a NodeInfo) isSetupOption()         {}
func (a NodeInfoPrivacy) isSetupOption()  {}
func (a AllowedPublicKey) isSetupOption() {}
