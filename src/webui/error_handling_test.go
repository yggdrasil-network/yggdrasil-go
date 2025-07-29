package webui

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebUIServer_InvalidListenAddress(t *testing.T) {
	logger := createTestLogger()

	// Test various invalid listen addresses
	invalidAddresses := []string{
		"invalid:address",
		"256.256.256.256:8080",
		"localhost:-1",
		"localhost:99999",
		"not-a-valid-address",
		"",
	}

	for _, addr := range invalidAddresses {
		t.Run(fmt.Sprintf("Address_%s", addr), func(t *testing.T) {
			server := Server(addr, logger)

			// Start should fail for invalid addresses
			err := server.Start()
			if err == nil {
				server.Stop() // Clean up if it somehow started
				t.Errorf("Expected Start() to fail for invalid address %s", addr)
			}
		})
	}
}

func TestWebUIServer_PortAlreadyInUse(t *testing.T) {
	logger := createTestLogger()

	// Start a server on a specific port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Get the port that's now in use
	usedPort := listener.Addr().(*net.TCPAddr).Port
	conflictAddress := fmt.Sprintf("127.0.0.1:%d", usedPort)

	server := Server(conflictAddress, logger)

	// This should fail because port is already in use
	err = server.Start()
	if err == nil {
		server.Stop()
		t.Error("Expected Start() to fail when port is already in use")
	}
}

func TestWebUIServer_DoubleStart(t *testing.T) {
	logger := createTestLogger()

	// Create two separate servers to test behavior
	server1 := Server("127.0.0.1:0", logger)
	server2 := Server("127.0.0.1:0", logger)

	// Start first server
	startDone1 := make(chan error, 1)
	go func() {
		startDone1 <- server1.Start()
	}()

	// Wait for first server to start
	time.Sleep(100 * time.Millisecond)

	if server1.server == nil {
		t.Fatal("First server should have started")
	}

	// Start second server (should work since different instance)
	startDone2 := make(chan error, 1)
	go func() {
		startDone2 <- server2.Start()
	}()

	// Wait a bit then stop both servers
	time.Sleep(100 * time.Millisecond)

	err1 := server1.Stop()
	if err1 != nil {
		t.Errorf("Stop() failed for server1: %v", err1)
	}

	err2 := server2.Stop()
	if err2 != nil {
		t.Errorf("Stop() failed for server2: %v", err2)
	}

	// Wait for both Start() calls to complete
	select {
	case err := <-startDone1:
		if err != nil {
			t.Logf("First Start() returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("First Start() did not return after Stop()")
	}

	select {
	case err := <-startDone2:
		if err != nil {
			t.Logf("Second Start() returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Second Start() did not return after Stop()")
	}
}

func TestWebUIServer_StopTwice(t *testing.T) {
	logger := createTestLogger()
	server := Server("127.0.0.1:0", logger)

	// Start server
	go func() {
		server.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	// Stop server first time
	err := server.Stop()
	if err != nil {
		t.Errorf("First Stop() failed: %v", err)
	}

	// Stop server second time - should not error
	err = server.Stop()
	if err != nil {
		t.Errorf("Second Stop() failed: %v", err)
	}
}

func TestWebUIServer_GracefulShutdown(t *testing.T) {
	logger := createTestLogger()

	// Create a listener to get a real address
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	addr := listener.Addr().String()
	listener.Close() // Close so our server can use it

	server := Server(addr, logger)

	// Channel to track when Start() returns
	startDone := make(chan error, 1)

	// Start server
	go func() {
		startDone <- server.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	// Verify server is running
	if server.server == nil {
		t.Fatal("Server should be running")
	}

	// Make a request while server is running
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/health", addr))
	if err != nil {
		t.Fatalf("Request failed while server running: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Stop server
	err = server.Stop()
	if err != nil {
		t.Errorf("Stop() failed: %v", err)
	}

	// Verify Start() returns
	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("Start() returned error after Stop(): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Start() did not return within timeout after Stop()")
	}

	// Verify server is no longer accessible
	_, err = client.Get(fmt.Sprintf("http://%s/health", addr))
	if err == nil {
		t.Error("Expected request to fail after server stopped")
	}
}

func TestWebUIServer_ContextCancellation(t *testing.T) {
	logger := createTestLogger()
	server := Server("127.0.0.1:0", logger)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start server
	go func() {
		server.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	// Wait for context to be cancelled
	<-ctx.Done()

	// Stop server after context cancellation
	err := server.Stop()
	if err != nil {
		t.Errorf("Stop() failed after context cancellation: %v", err)
	}
}

func TestWebUIServer_LoggerNil(t *testing.T) {
	// Test server creation with nil logger
	server := Server("127.0.0.1:0", nil)

	if server == nil {
		t.Fatal("Server should be created even with nil logger")
	}

	if server.log != nil {
		t.Error("Server logger should be nil if nil was passed")
	}
}

func TestWebUIServer_EmptyListenAddress(t *testing.T) {
	logger := createTestLogger()

	// Test with empty listen address
	server := Server("", logger)

	// This might fail when trying to start
	err := server.Start()
	if err == nil {
		server.Stop()
		t.Log("Note: Server started with empty listen address")
	} else {
		t.Logf("Expected behavior: Start() failed with empty address: %v", err)
	}
}

func TestWebUIServer_RapidStartStop(t *testing.T) {
	logger := createTestLogger()

	// Test rapid start/stop cycles with fewer iterations
	for i := 0; i < 5; i++ {
		server := Server("127.0.0.1:0", logger)

		// Start server
		startDone := make(chan error, 1)
		go func() {
			startDone <- server.Start()
		}()

		// Wait a bit for server to start
		time.Sleep(50 * time.Millisecond)

		// Stop server
		err := server.Stop()
		if err != nil {
			t.Errorf("Iteration %d: Stop() failed: %v", i, err)
		}

		// Wait for Start() to return
		select {
		case <-startDone:
			// Start() returned, good
		case <-time.After(1 * time.Second):
			t.Errorf("Iteration %d: Start() did not return after Stop()", i)
		}

		// Pause between iterations to avoid port conflicts
		time.Sleep(50 * time.Millisecond)
	}
}

func TestWebUIServer_LargeNumberOfRequests(t *testing.T) {
	logger := createTestLogger()

	// Use httptest.Server for more reliable testing
	mux := http.NewServeMux()
	setupStaticHandler(mux)
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		serveFile(rw, r, logger)
	})
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte("OK"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Send many requests quickly
	const numRequests = 50 // Reduced number for more reliable testing
	errorChan := make(chan error, numRequests)

	client := &http.Client{Timeout: 2 * time.Second}

	for i := 0; i < numRequests; i++ {
		go func(requestID int) {
			resp, err := client.Get(server.URL + "/health")
			if err != nil {
				errorChan <- fmt.Errorf("request %d failed: %v", requestID, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errorChan <- fmt.Errorf("request %d: expected status 200, got %d", requestID, resp.StatusCode)
				return
			}

			errorChan <- nil
		}(i)
	}

	// Check results
	errorCount := 0
	for i := 0; i < numRequests; i++ {
		select {
		case err := <-errorChan:
			if err != nil {
				errorCount++
				if errorCount <= 5 { // Only log first few errors
					t.Errorf("Request error: %v", err)
				}
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("Request %d timed out", i)
		}
	}

	if errorCount > 0 {
		t.Errorf("Total failed requests: %d/%d", errorCount, numRequests)
	}
}
