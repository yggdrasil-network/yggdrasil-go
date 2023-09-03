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
