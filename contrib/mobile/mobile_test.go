package mobile

import "testing"

func TestStartYggdrasil(t *testing.T) {
	mesh := &Mesh{}
	if err := mesh.StartAutoconfigure(); err != nil {
		t.Fatalf("Failed to start Mesh: %s", err)
	}
	t.Log("Address:", mesh.GetAddressString())
	t.Log("Subnet:", mesh.GetSubnetString())
	t.Log("Coords:", mesh.GetCoordsString())
	if err := mesh.Stop(); err != nil {
		t.Fatalf("Failed to stop Yggdrasil: %s", err)
	}
}
