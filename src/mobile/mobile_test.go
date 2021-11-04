package mobile

import "testing"

func TestStartYggdrasil(t *testing.T) {
	ygg := &Yggdrasil{}
	if err := ygg.StartAutoconfigure(); err != nil {
		t.Fatalf("Failed to start Yggdrasil: %s", err)
	}
	t.Log("Address:", ygg.GetAddressString())
	t.Log("Subnet:", ygg.GetSubnetString())
	t.Log("Coords:", ygg.GetCoordsString())
	if err := ygg.Stop(); err != nil {
		t.Fatalf("Failed to stop Yggdrasil: %s", err)
	}
}
