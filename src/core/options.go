package core

import (
	"crypto/ed25519"
)

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
