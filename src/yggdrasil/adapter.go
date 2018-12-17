package yggdrasil

// Defines the minimum required functions for an adapter type.
type AdapterInterface interface {
	init(core *Core, send chan<- []byte, recv <-chan []byte)
	read() error
	write() error
	close() error
}

// Defines the minimum required struct members for an adapter type (this is
// now the base type for tunAdapter in tun.go)
type Adapter struct {
	AdapterInterface
	core *Core
	send chan<- []byte
	recv <-chan []byte
}

// Initialises the adapter.
func (adapter *Adapter) init(core *Core, send chan<- []byte, recv <-chan []byte) {
	adapter.core = core
	adapter.send = send
	adapter.recv = recv
}
