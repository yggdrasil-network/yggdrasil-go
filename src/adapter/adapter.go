package adapter

import (
	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// Defines the minimum required struct members for an adapter type (this is
// now the base type for TunAdapter in tun.go)
type Adapter struct {
	Config      *config.StatefulNodeConfig
	Log         *log.Logger
	Send        chan<- []byte
	Recv        <-chan []byte
	Reconfigure chan chan error
}

// Initialises the adapter.
func (adapter *Adapter) Init(config *config.StatefulNodeConfig, log *log.Logger, send chan<- []byte, recv <-chan []byte) {
	adapter.Config = config
	adapter.Log = log
	adapter.Send = send
	adapter.Recv = recv
	adapter.Reconfigure = make(chan chan error, 1)
}
