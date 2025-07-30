package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gologme/log"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// Helper function to create a test logger
func createTestLogger() core.Logger {
	return log.New(os.Stderr, "webui_test: ", log.Flags())
}

// Helper function to get available port for testing
func getTestAddress() string {
	return "127.0.0.1:0" // Let OS assign available port
}

func TestWebUIServer_Creation(t *testing.T) {
	logger := createTestLogger()
	listen := getTestAddress()

	server := Server(listen, "", logger)

	if server == nil {
		t.Fatal("Server function returned nil")
	}

	if server.listen != listen {
		t.Errorf("Expected listen address %s, got %s", listen, server.listen)
	}

	if server.log != logger {
		t.Error("Logger not properly set")
	}

	if server.server != nil {
		t.Error("HTTP server should be nil before Start()")
	}
}

func TestWebUIServer_StartStop(t *testing.T) {
	logger := createTestLogger()
	listen := getTestAddress()

	server := Server(listen, "", logger)

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is running
	if server.server == nil {
		t.Fatal("HTTP server not initialized after Start()")
	}

	// Stop server
	err := server.Stop()
	if err != nil {
		t.Errorf("Error stopping server: %v", err)
	}

	// Check that Start() returns without error after Stop()
	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Start() returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Start() did not return after Stop()")
	}
}

func TestWebUIServer_StopWithoutStart(t *testing.T) {
	logger := createTestLogger()
	listen := getTestAddress()

	server := Server(listen, "", logger)

	// Stop server that was never started should not error
	err := server.Stop()
	if err != nil {
		t.Errorf("Stop() on unstarted server returned error: %v", err)
	}
}

func TestWebUIServer_HealthEndpoint(t *testing.T) {
	logger := createTestLogger()

	// Create a test server using net/http/httptest for reliable testing
	mux := http.NewServeMux()
	testServer := Server("127.0.0.1:0", "", logger)
	setupStaticHandler(mux, testServer)
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		serveFile(rw, r, logger)
	})
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("OK"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test health endpoint
	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("Error requesting health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}

	if string(body) != "OK" {
		t.Errorf("Expected body 'OK', got '%s'", string(body))
	}
}

func TestWebUIServer_Timeouts(t *testing.T) {
	logger := createTestLogger()
	server := Server("127.0.0.1:0", "", logger)

	// Start server
	go func() {
		_ = server.Start()
	}()
	defer func() { _ = server.Stop() }()

	// Wait for server to start
	time.Sleep(200 * time.Millisecond)

	if server.server == nil {
		t.Fatal("Server not started")
	}

	// Check that timeouts are properly configured
	expectedReadTimeout := 10 * time.Second
	expectedWriteTimeout := 10 * time.Second
	expectedMaxHeaderBytes := 1 << 20

	if server.server.ReadTimeout != expectedReadTimeout {
		t.Errorf("Expected ReadTimeout %v, got %v", expectedReadTimeout, server.server.ReadTimeout)
	}

	if server.server.WriteTimeout != expectedWriteTimeout {
		t.Errorf("Expected WriteTimeout %v, got %v", expectedWriteTimeout, server.server.WriteTimeout)
	}

	if server.server.MaxHeaderBytes != expectedMaxHeaderBytes {
		t.Errorf("Expected MaxHeaderBytes %d, got %d", expectedMaxHeaderBytes, server.server.MaxHeaderBytes)
	}
}

func TestWebUIServer_ConcurrentStartStop(t *testing.T) {
	logger := createTestLogger()

	// Test concurrent start/stop operations with separate servers
	for i := 0; i < 3; i++ {
		server := Server("127.0.0.1:0", "", logger)

		// Start server
		startDone := make(chan error, 1)
		go func() {
			startDone <- server.Start()
		}()

		time.Sleep(100 * time.Millisecond)

		// Stop server
		err := server.Stop()
		if err != nil {
			t.Errorf("Iteration %d: Error stopping server: %v", i, err)
		}

		// Wait for Start() to return
		select {
		case <-startDone:
			// Good, Start() returned
		case <-time.After(2 * time.Second):
			t.Errorf("Iteration %d: Start() did not return after Stop()", i)
		}

		time.Sleep(50 * time.Millisecond)
	}
}
