package dummy

import (
	"github.com/gologme/log"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

// DummyAdapter is a non-specific adapter that is used by the mobile APIs.
// You can also use it to send or receive custom traffic over Yggdrasil.
type DummyAdapter struct {
	yggdrasil.Adapter
}

// Init initialises the dummy adapter.
func (m *DummyAdapter) Init(config *config.NodeState, log *log.Logger, send chan<- []byte, recv <-chan []byte, reject <-chan yggdrasil.RejectedPacket) {
	m.Adapter.Init(config, log, send, recv, reject)
}

// Name returns the name of the adapter. This is always "dummy" for dummy
// adapters.
func (m *DummyAdapter) Name() string {
	return "dummy"
}

// MTU gets the adapter's MTU. This returns your platform's maximum MTU for
// dummy adapters.
func (m *DummyAdapter) MTU() int {
	return defaults.GetDefaults().MaximumIfMTU
}

// IsTAP always returns false for dummy adapters.
func (m *DummyAdapter) IsTAP() bool {
	return false
}

// Recv waits for and returns for a packet from the router.
func (m *DummyAdapter) Recv() ([]byte, error) {
	packet := <-m.Adapter.Recv
	return packet, nil
}

// Send a packet to the router.
func (m *DummyAdapter) Send(buf []byte) error {
	packet := append(util.GetBytes(), buf[:]...)
	m.Adapter.Send <- packet
	return nil
}

// Start is not implemented for dummy adapters.
func (m *DummyAdapter) Start(address.Address, address.Subnet) error {
	return nil
}

// Close is not implemented for dummy adapters.
func (m *DummyAdapter) Close() error {
	return nil
}
