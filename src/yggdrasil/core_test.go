package yggdrasil

import (
	"bytes"
	"math/rand"
	"os"
	"testing"
	"time"

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

func CreateAndConnectTwo(t testing.TB) (*Core, *Core) {
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

	if l := len(nodeA.GetPeers()); l != 1 {
		t.Fatal("unexpected number of peers", l)
	}
	if l := len(nodeB.GetPeers()); l != 1 {
		t.Fatal("unexpected number of peers", l)
	}

	return &nodeA, &nodeB
}

// WaitConnected blocks until either nodes negotiated DHT or 5 seconds passed.
func WaitConnected(nodeA, nodeB *Core) bool {
	// It may take up to 3 seconds, but let's wait 5.
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if len(nodeA.GetSwitchPeers()) > 0 && len(nodeB.GetSwitchPeers()) > 0 {
			return true
		}
	}
	return false
}

func TestCore_Start_Connect(t *testing.T) {
	CreateAndConnectTwo(t)
}

func CreateEchoListener(t testing.TB, nodeA *Core, bufLen int, repeats int) chan struct{} {
	// Listen
	listener, err := nodeA.ConnListen()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		buf := make([]byte, bufLen)

		for i := 0; i < repeats; i++ {
			n, err := conn.Read(buf)
			if err != nil {
				t.Error(err)
				return
			}
			if n != bufLen {
				t.Error("missing data")
				return
			}
			_, err = conn.Write(buf)
			if err != nil {
				t.Error(err)
			}
		}
		done <- struct{}{}
	}()

	return done
}

func TestCore_Start_Transfer(t *testing.T) {
	nodeA, nodeB := CreateAndConnectTwo(t)

	msgLen := 1500
	done := CreateEchoListener(t, nodeA, msgLen, 1)

	if !WaitConnected(nodeA, nodeB) {
		t.Fatal("nodes did not connect")
	}

	// Dial
	dialer, err := nodeB.ConnDialer()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := dialer.Dial("nodeid", nodeA.NodeID().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	msg := make([]byte, msgLen)
	rand.Read(msg)
	conn.Write(msg)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, msgLen)
	_, err = conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(msg, buf) != 0 {
		t.Fatal("expected echo")
	}
	<-done
}

func BenchmarkCore_Start_Transfer(b *testing.B) {
	nodeA, nodeB := CreateAndConnectTwo(b)

	msgLen := 1500 // typical MTU
	done := CreateEchoListener(b, nodeA, msgLen, b.N)

	if !WaitConnected(nodeA, nodeB) {
		b.Fatal("nodes did not connect")
	}

	// Dial
	dialer, err := nodeB.ConnDialer()
	if err != nil {
		b.Fatal(err)
	}
	conn, err := dialer.Dial("nodeid", nodeA.NodeID().String())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	msg := make([]byte, msgLen)
	rand.Read(msg)
	buf := make([]byte, msgLen)

	b.SetBytes(int64(b.N * msgLen))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		conn.Write(msg)
		if err != nil {
			b.Fatal(err)
		}
		_, err = conn.Read(buf)
		if err != nil {
			b.Fatal(err)
		}
	}
	<-done
}
