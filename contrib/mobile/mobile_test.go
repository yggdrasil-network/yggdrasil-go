package mobile

import (
	"os"
	"testing"

	"github.com/gologme/log"
)

func TestStartYggdrasil(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	logger.EnableLevel("error")
	logger.EnableLevel("warn")
	logger.EnableLevel("info")

	ygg := &Yggdrasil{
		logger: logger,
	}
	if err := ygg.StartAutoconfigure(); err != nil {
		t.Fatalf("Failed to start Yggdrasil: %s", err)
	}
	t.Log("Address:", ygg.GetAddressString())
	t.Log("Subnet:", ygg.GetSubnetString())
	t.Log("Routing entries:", ygg.GetRoutingEntries())
	if err := ygg.Stop(); err != nil {
		t.Fatalf("Failed to stop Yggdrasil: %s", err)
	}
}

// SendBuffer previously panicked when the caller passed a negative
// length (p[:length] out of range) and Send/SendBuffer with empty
// payload reached writePC, which also panicked.
func TestSendBufferRejectsBadLength(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	ygg := &Yggdrasil{logger: logger}
	if err := ygg.StartAutoconfigure(); err != nil {
		t.Fatalf("Failed to start Yggdrasil: %s", err)
	}
	defer func() { _ = ygg.Stop() }()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SendBuffer must not panic on bad length, got: %v", r)
		}
	}()
	if err := ygg.SendBuffer([]byte{1, 2, 3, 4}, -1); err != nil {
		t.Fatalf("SendBuffer returned unexpected error: %s", err)
	}
	if err := ygg.SendBuffer(nil, 0); err != nil {
		t.Fatalf("SendBuffer returned unexpected error: %s", err)
	}
	if err := ygg.Send(nil); err != nil {
		t.Fatalf("Send returned unexpected error: %s", err)
	}
}
