package yggdrasil

import (
	"github.com/gologme/log"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// Defines the minimum required struct members for an adapter type. This is now
// the base type for adapters like tun.go. When implementing a new adapter type,
// you should extend the adapter struct with this one and should call the
// Adapter.Init() function when initialising.
type Adapter struct {
	adapterImplementation
	Core        *Core
	Send        chan<- []byte
	Recv        <-chan []byte
	Reject      <-chan RejectedPacket
	Reconfigure chan chan error
}

// Defines the minimum required functions for an adapter type. Note that the
// implementation of Init() should call Adapter.Init(). This is not exported
// because doing so breaks the gomobile bindings for iOS/Android.
type adapterImplementation interface {
	Init(*config.NodeState, *log.Logger, chan<- []byte, <-chan []byte, <-chan RejectedPacket)
	Name() string
	MTU() int
	IsTAP() bool
	Start(address.Address, address.Subnet) error
	Close() error
}

// Initialises the adapter with the necessary channels to operate from the
// router. When defining a new Adapter type, the Adapter should call this
// function from within it's own Init function to set up the channels. It is
// otherwise not expected for you to call this function directly.
func (adapter *Adapter) Init(config *config.NodeState, log *log.Logger, send chan<- []byte, recv <-chan []byte, reject <-chan RejectedPacket) {
	adapter.Send = send
	adapter.Recv = recv
	adapter.Reject = reject
	adapter.Reconfigure = make(chan chan error, 1)
}
