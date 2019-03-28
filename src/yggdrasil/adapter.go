package yggdrasil

import (
	"github.com/gologme/log"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// Defines the minimum required struct members for an adapter type (this is
// now the base type for TunAdapter in tun.go)
type Adapter struct {
	adapterImplementation
	Core        *Core
	Send        chan<- []byte
	Recv        <-chan []byte
	Reconfigure chan chan error
}

// Defines the minimum required functions for an adapter type
type adapterImplementation interface {
	Init(*config.NodeState, *log.Logger, chan<- []byte, <-chan []byte)
	Name() string
	MTU() int
	IsTAP() bool
	Start(address.Address, address.Subnet) error
	Read() error
	Write() error
	Close() error
}

// Initialises the adapter.
func (adapter *Adapter) Init(config *config.NodeState, log *log.Logger, send chan<- []byte, recv <-chan []byte) {
	log.Traceln("Adapter setup - given channels:", send, recv)
	adapter.Send = send
	adapter.Recv = recv
	adapter.Reconfigure = make(chan chan error, 1)
}
