package core

import (
	"net/url"
	"testing"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// Tests that duplicate peers in the configuration file
// won't cause an error when the node starts. Otherwise
// we can panic unnecessarily.
func TestDuplicatePeerAtStartup(t *testing.T) {
	cfg := config.GenerateConfig()
	for i := 0; i < 5; i++ {
		cfg.Peers = append(cfg.Peers, "tcp://1.2.3.4:4321")
	}
	if _, err := New(cfg.Certificate, nil); err != nil {
		t.Fatal(err)
	}
}

// Tests that duplicate peers given to us through the
// API will still error as expected, even if they didn't
// at startup. We expect to notify the user through the
// admin socket if they try to add a peer that is already
// configured.
func TestDuplicatePeerFromAPI(t *testing.T) {
	cfg := config.GenerateConfig()
	c, err := New(cfg.Certificate, nil)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse("tcp://1.2.3.4:4321")
	if err := c.AddPeer(u, ""); err != nil {
		t.Fatalf("Adding peer failed on first attempt: %s", err)
	}
	if err := c.AddPeer(u, ""); err == nil {
		t.Fatalf("Adding peer should have failed on second attempt")
	}
}
