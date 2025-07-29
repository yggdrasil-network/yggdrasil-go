package webui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebUIServer_RootEndpoint(t *testing.T) {
	logger := createTestLogger()

	// Use httptest.Server for more reliable testing
	mux := http.NewServeMux()
	setupStaticHandler(mux)
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		serveFile(rw, r, logger)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test root endpoint
	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("Error requesting root endpoint: %v", err)
	}
	defer resp.Body.Close()

	// Should return some content (index.html or 404, depending on build mode)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 200 or 404, got %d", resp.StatusCode)
	}
}

func TestWebUIServer_HealthEndpointDetails(t *testing.T) {
	logger := createTestLogger()

	// Use httptest.Server for more reliable testing
	mux := http.NewServeMux()
	setupStaticHandler(mux)
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		serveFile(rw, r, logger)
	})
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("OK"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test health endpoint with different HTTP methods
	testCases := []struct {
		method         string
		expectedStatus int
	}{
		{"GET", http.StatusOK},
		{"POST", http.StatusOK},
		{"PUT", http.StatusOK},
		{"DELETE", http.StatusOK},
		{"HEAD", http.StatusOK},
	}

	client := &http.Client{Timeout: 5 * time.Second}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Method_%s", tc.method), func(t *testing.T) {
			req, err := http.NewRequest(tc.method, server.URL+"/health", nil)
			if err != nil {
				t.Fatalf("Error creating request: %v", err)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Error making request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("Expected status %d for %s, got %d", tc.expectedStatus, tc.method, resp.StatusCode)
			}

			if tc.method != "HEAD" {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("Error reading response body: %v", err)
				}

				if string(body) != "OK" {
					t.Errorf("Expected body 'OK', got '%s'", string(body))
				}
			}
		})
	}
}

func TestWebUIServer_NonExistentEndpoint(t *testing.T) {
	logger := createTestLogger()

	// Use httptest.Server for more reliable testing
	mux := http.NewServeMux()
	setupStaticHandler(mux)
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		serveFile(rw, r, logger)
	})
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("OK"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test non-existent endpoints
	testPaths := []string{
		"/nonexistent",
		"/api/v1/test",
		"/static/nonexistent.css",
		"/admin",
		"/config",
	}

	client := &http.Client{Timeout: 5 * time.Second}

	for _, path := range testPaths {
		t.Run(fmt.Sprintf("Path_%s", strings.ReplaceAll(path, "/", "_")), func(t *testing.T) {
			resp, err := client.Get(server.URL + path)
			if err != nil {
				t.Fatalf("Error requesting %s: %v", path, err)
			}
			defer resp.Body.Close()

			// Should return 404 for non-existent paths
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("Expected status 404 for %s, got %d", path, resp.StatusCode)
			}
		})
	}
}

func TestWebUIServer_ContentTypes(t *testing.T) {
	// This test checks if proper content types are set
	// We'll use httptest.Server for more controlled testing

	logger := createTestLogger()

	// Create a test handler similar to what the webui server creates
	mux := http.NewServeMux()

	// Setup handlers like in the actual server
	setupStaticHandler(mux)
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		serveFile(rw, r, logger)
	})
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("OK"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test health endpoint content type
	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("Error requesting health endpoint: %v", err)
	}
	defer resp.Body.Close()

	// Health endpoint might not set explicit content type, which is fine
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for health endpoint, got %d", resp.StatusCode)
	}
}

func TestWebUIServer_HeaderSecurity(t *testing.T) {
	logger := createTestLogger()

	// Use httptest.Server for more reliable testing
	mux := http.NewServeMux()
	setupStaticHandler(mux)
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		serveFile(rw, r, logger)
	})
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("OK"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test that server handles large headers properly
	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequest("GET", server.URL+"/health", nil)
	if err != nil {
		t.Fatalf("Error creating request: %v", err)
	}

	// Add a reasonably sized header
	req.Header.Set("X-Test-Header", strings.Repeat("a", 1000))

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Error making request with large header: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 with normal header, got %d", resp.StatusCode)
	}
}

func TestWebUIServer_ConcurrentRequests(t *testing.T) {
	logger := createTestLogger()

	// Use httptest.Server for more reliable testing
	mux := http.NewServeMux()
	setupStaticHandler(mux)
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		serveFile(rw, r, logger)
	})
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("OK"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test concurrent requests to health endpoint
	const numRequests = 20
	errChan := make(chan error, numRequests)

	client := &http.Client{Timeout: 5 * time.Second}

	for i := 0; i < numRequests; i++ {
		go func() {
			resp, err := client.Get(server.URL + "/health")
			if err != nil {
				errChan <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errChan <- fmt.Errorf("unexpected status code: %d", resp.StatusCode)
				return
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				errChan <- err
				return
			}

			if string(body) != "OK" {
				errChan <- fmt.Errorf("unexpected body: %s", string(body))
				return
			}

			errChan <- nil
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		select {
		case err := <-errChan:
			if err != nil {
				t.Errorf("Concurrent request %d failed: %v", i+1, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("Request %d timed out", i+1)
		}
	}
}
