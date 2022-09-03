package core

import (
	"crypto/ed25519"
)

func (c *Core) _applyOption(opt SetupOption) {
	switch v := opt.(type) {
	case Peer:
		c.config._peers[v] = struct{}{}
	case ListenAddress:
		c.config._listeners[v] = struct{}{}
	case NodeInfo:
		c.config.nodeinfo = v
	case NodeInfoPrivacy:
		c.config.nodeinfoPrivacy = v
	case IfName:
		c.config.ifname = v
	case IfMTU:
		c.config.ifmtu = v
	case AllowedPublicKey:
		pk := [32]byte{}
		copy(pk[:], v)
		c.config._allowedPublicKeys[pk] = struct{}{}
	}
}

type SetupOption interface {
	isSetupOption()
}

type ListenAddress string
type AdminListenAddress string
type Peer struct {
	URI             string
	SourceInterface string
}
type NodeInfo map[string]interface{}
type NodeInfoPrivacy bool
type IfName string
type IfMTU uint16
type AllowedPublicKey ed25519.PublicKey

func (a ListenAddress) isSetupOption()      {}
func (a AdminListenAddress) isSetupOption() {}
func (a Peer) isSetupOption()               {}
func (a NodeInfo) isSetupOption()           {}
func (a NodeInfoPrivacy) isSetupOption()    {}
func (a IfName) isSetupOption()             {}
func (a IfMTU) isSetupOption()              {}
func (a AllowedPublicKey) isSetupOption()   {}
