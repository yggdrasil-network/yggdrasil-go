package yggdrasil

import (
	"os"
	"testing"

	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// GenerateConfig is modification
func GenerateConfig() *config.NodeConfig {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	cfg.Listen = []string{"tcp://127.0.0.1:0"}
	cfg.IfName = "none"

	return cfg
}

func GetLoggerWithPrefix(prefix string) *log.Logger {
	l := log.New(os.Stderr, prefix, log.Flags())
	l.EnableLevel("info")
	l.EnableLevel("warn")
	l.EnableLevel("error")
	return l
}

func TestCore_Start(t *testing.T) {
	nodeA := Core{}
	_, err := nodeA.Start(GenerateConfig(), GetLoggerWithPrefix("A: "))
	if err != nil {
		t.Fatal(err)
	}

	nodeB := Core{}
	_, err = nodeB.Start(GenerateConfig(), GetLoggerWithPrefix("B: "))
	if err != nil {
		t.Fatal(err)
	}

	err = nodeB.AddPeer("tcp://"+nodeA.link.tcp.getAddr().String(), "")
	if err != nil {
		t.Fatal(err)
	}
}
