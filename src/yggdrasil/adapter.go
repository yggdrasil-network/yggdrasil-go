package yggdrasil

// Defines the minimum required struct members for an adapter type (this is
// now the base type for tunAdapter in tun.go)
type Adapter struct {
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
