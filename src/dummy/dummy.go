package dummy

import (
	"github.com/gologme/log"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
	"github.com/yggdrasil-network/yggdrasil-go/src/yggdrasil"
)

type DummyAdapter struct {
	yggdrasil.Adapter
	send   chan<- []byte
	recv   <-chan []byte
	reject <-chan yggdrasil.RejectedPacket
}

// Init initialises the TUN/TAP adapter.
func (m *DummyAdapter) Init(config *config.NodeState, log *log.Logger, send chan<- []byte, recv <-chan []byte, reject <-chan yggdrasil.RejectedPacket) {
	m.Adapter.Init(config, log, send, recv, reject)
}

// Name returns the name of the adapter, e.g. "tun0". On Windows, this may
// return a canonical adapter name instead.
func (m *DummyAdapter) Name() string {
	return "dummy"
}

// MTU gets the adapter's MTU. This can range between 1280 and 65535, although
// the maximum value is determined by your platform. The returned value will
// never exceed that of MaximumMTU().
func (m *DummyAdapter) MTU() int {
	return 65535
}

// IsTAP returns true if the adapter is a TAP adapter (Layer 2) or false if it
// is a TUN adapter (Layer 3).
func (m *DummyAdapter) IsTAP() bool {
	return false
}

// Wait for a packet from the router. You will use this when implementing a
// dummy adapter in place of real TUN - when this call returns a packet, you
// will probably want to give it to the OS to write to TUN.
func (m *DummyAdapter) Recv() ([]byte, error) {
	packet := <-m.recv
	return packet, nil
}

// Send a packet to the router. You will use this when implementing a
// dummy adapter in place of real TUN - when the operating system tells you
// that a new packet is available from TUN, call this function to give it to
// Yggdrasil.
func (m *DummyAdapter) Send(buf []byte) error {
	packet := append(util.GetBytes(), buf[:]...)
	m.send <- packet
	return nil
}
