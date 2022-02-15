package types

import "testing"

func TestEndpointMappings(t *testing.T) {
	var mappings TCPMappings
	if err := mappings.Set("1234"); err != nil {
		t.Fatal(err)
	}
	if err := mappings.Set("1234:192.168.1.1"); err != nil {
		t.Fatal(err)
	}
	if err := mappings.Set("1234:192.168.1.1:4321"); err != nil {
		t.Fatal(err)
	}
	if err := mappings.Set("1234:[2000::1]:4321"); err != nil {
		t.Fatal(err)
	}
	if err := mappings.Set("a"); err == nil {
		t.Fatal("'a' should be an invalid exposed port")
	}
	if err := mappings.Set("1234:localhost"); err == nil {
		t.Fatal("mapped address must be an IP literal")
	}
	if err := mappings.Set("1234:localhost:a"); err == nil {
		t.Fatal("'a' should be an invalid mapped port")
	}
}
