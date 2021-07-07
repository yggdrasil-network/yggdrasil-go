package core

import (
	"bytes"
	"math/rand"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
)

// GenerateConfig produces default configuration with suitable modifications for tests.
func GenerateConfig() *config.NodeConfig {
	cfg := defaults.GenerateConfig()
	cfg.AdminListen = "none"
	cfg.Listen = []string{"tcp://127.0.0.1:0"}
	cfg.IfName = "none"

	return cfg
}

// GetLoggerWithPrefix creates a new logger instance with prefix.
// If verbose is set to true, three log levels are enabled: "info", "warn", "error".
func GetLoggerWithPrefix(prefix string, verbose bool) *log.Logger {
	l := log.New(os.Stderr, prefix, log.Flags())
	if !verbose {
		return l
	}
	l.EnableLevel("info")
	l.EnableLevel("warn")
	l.EnableLevel("error")
	return l
}

// CreateAndConnectTwo creates two nodes. nodeB connects to nodeA.
// Verbosity flag is passed to logger.
func CreateAndConnectTwo(t testing.TB, verbose bool) (nodeA *Core, nodeB *Core) {
	nodeA = new(Core)
	if err := nodeA.Start(GenerateConfig(), GetLoggerWithPrefix("A: ", verbose)); err != nil {
		t.Fatal(err)
	}

	nodeB = new(Core)
	if err := nodeB.Start(GenerateConfig(), GetLoggerWithPrefix("B: ", verbose)); err != nil {
		t.Fatal(err)
	}

	u, err := url.Parse("tcp://" + nodeA.links.tcp.getAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	err = nodeB.CallPeer(u, "")
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	if l := len(nodeA.GetPeers()); l != 1 {
		t.Fatal("unexpected number of peers", l)
	}
	if l := len(nodeB.GetPeers()); l != 1 {
		t.Fatal("unexpected number of peers", l)
	}

	return nodeA, nodeB
}

// WaitConnected blocks until either nodes negotiated DHT or 5 seconds passed.
func WaitConnected(nodeA, nodeB *Core) bool {
	// It may take up to 3 seconds, but let's wait 5.
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if len(nodeA.GetPeers()) > 0 && len(nodeB.GetPeers()) > 0 {
			return true
		}
	}
	return false
}

// CreateEchoListener creates a routine listening on nodeA. It expects repeats messages of length bufLen.
// It returns a channel used to synchronize the routine with caller.
func CreateEchoListener(t testing.TB, nodeA *Core, bufLen int, repeats int) chan struct{} {
	// Start routine
	done := make(chan struct{})
	go func() {
		buf := make([]byte, bufLen)
		res := make([]byte, bufLen)
		for i := 0; i < repeats; i++ {
			n, from, err := nodeA.ReadFrom(buf)
			if err != nil {
				t.Error(err)
				return
			}
			if n != bufLen {
				t.Error("missing data")
				return
			}
			copy(res, buf)
			copy(res[8:24], buf[24:40])
			copy(res[24:40], buf[8:24])
			_, err = nodeA.WriteTo(res, from)
			if err != nil {
				t.Error(err)
			}
		}
		done <- struct{}{}
	}()

	return done
}

// TestCore_Start_Connect checks if two nodes can connect together.
func TestCore_Start_Connect(t *testing.T) {
	CreateAndConnectTwo(t, true)
}

// TestCore_Start_Transfer checks that messages can be passed between nodes (in both directions).
func TestCore_Start_Transfer(t *testing.T) {
	nodeA, nodeB := CreateAndConnectTwo(t, true)
	defer nodeA.Stop()
	defer nodeB.Stop()

	msgLen := 1500
	done := CreateEchoListener(t, nodeA, msgLen, 1)

	if !WaitConnected(nodeA, nodeB) {
		t.Fatal("nodes did not connect")
	}

	// Send
	msg := make([]byte, msgLen)
	rand.Read(msg[40:])
	msg[0] = 0x60
	copy(msg[8:24], nodeB.Address())
	copy(msg[24:40], nodeA.Address())
	_, err := nodeB.WriteTo(msg, nodeA.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, msgLen)
	_, _, err = nodeB.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(msg[40:], buf[40:]) {
		t.Fatal("expected echo")
	}
	<-done
}

// BenchmarkCore_Start_Transfer estimates the possible transfer between nodes (in MB/s).
func BenchmarkCore_Start_Transfer(b *testing.B) {
	nodeA, nodeB := CreateAndConnectTwo(b, false)

	msgLen := 1500 // typical MTU
	done := CreateEchoListener(b, nodeA, msgLen, b.N)

	if !WaitConnected(nodeA, nodeB) {
		b.Fatal("nodes did not connect")
	}

	// Send
	msg := make([]byte, msgLen)
	rand.Read(msg[40:])
	msg[0] = 0x60
	copy(msg[8:24], nodeB.Address())
	copy(msg[24:40], nodeA.Address())

	buf := make([]byte, msgLen)

	b.SetBytes(int64(msgLen))
	b.ResetTimer()

	addr := nodeA.LocalAddr()
	for i := 0; i < b.N; i++ {
		_, err := nodeB.WriteTo(msg, addr)
		if err != nil {
			b.Fatal(err)
		}
		_, _, err = nodeB.ReadFrom(buf)
		if err != nil {
			b.Fatal(err)
		}
	}
	<-done
}
