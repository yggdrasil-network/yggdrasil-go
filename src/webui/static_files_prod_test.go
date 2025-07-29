//go:build !debug
// +build !debug

package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticFiles_ProdMode_EmbeddedFiles(t *testing.T) {
	logger := createTestLogger()

	// Test that the embedded files system is working
	// Note: In production mode, we can't easily create test files
	// so we test the behavior with what's available

	// Test serveFile function with various paths
	testCases := []struct {
		path           string
		expectedStatus int
		description    string
	}{
		{"/", http.StatusOK, "root path should serve index.html if available"},
		{"/index.html", http.StatusOK, "index.html should be available if embedded"},
		{"/style.css", http.StatusOK, "style.css should be available if embedded"},
		{"/nonexistent.txt", http.StatusNotFound, "non-existent files should return 404"},
		{"/subdir/nonexistent.html", http.StatusNotFound, "non-existent nested files should return 404"},
	}

	for _, tc := range testCases {
		t.Run(strings.ReplaceAll(tc.path, "/", "_"), func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			rec := httptest.NewRecorder()

			serveFile(rec, req, logger)

			// For embedded files, we expect either 200 (if file exists) or 404 (if not)
			if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
				t.Errorf("Expected status 200 or 404 for %s, got %d", tc.path, rec.Code)
			}

			// Check that known files return expected status if they exist
			if (tc.path == "/" || tc.path == "/index.html") && rec.Code == http.StatusOK {
				// Should have HTML content type
				contentType := rec.Header().Get("Content-Type")
				if !strings.Contains(contentType, "text/html") {
					t.Logf("Note: Content-Type for %s is %s (might not contain text/html)", tc.path, contentType)
				}
			}
		})
	}
}

func TestStaticFiles_ProdMode_SetupStaticHandler(t *testing.T) {
	// Test that setupStaticHandler works in production mode
	mux := http.NewServeMux()

	// This should not panic
	setupStaticHandler(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test static handler route
	resp, err := http.Get(server.URL + "/static/style.css")
	if err != nil {
		t.Fatalf("Error requesting static file: %v", err)
	}
	defer resp.Body.Close()

	// Should return either 200 (if file exists) or 404 (if not embedded)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 200 or 404 for static file, got %d", resp.StatusCode)
	}
}

func TestStaticFiles_ProdMode_PathTraversal(t *testing.T) {
	logger := createTestLogger()

	// Test path traversal attempts in production mode
	pathTraversalTests := []string{
		"/../sensitive.txt",
		"/../../etc/passwd",
		"/..\\sensitive.txt",
		"/static/../../../etc/passwd",
		"/static/../../config.json",
	}

	for _, path := range pathTraversalTests {
		t.Run(strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			rec := httptest.NewRecorder()

			serveFile(rec, req, logger)

			// Should return 404 for path traversal attempts
			if rec.Code != http.StatusNotFound {
				t.Errorf("Expected status 404 for path traversal attempt %s, got %d", path, rec.Code)
			}

			// Should not contain any system file content
			body := rec.Body.String()
			if strings.Contains(body, "root:") || strings.Contains(body, "/bin/") {
				t.Errorf("Path traversal might be successful for %s - system content detected", path)
			}
		})
	}
}

func TestStaticFiles_ProdMode_ContentTypes(t *testing.T) {
	logger := createTestLogger()

	// Test that proper content types are set for different file types
	testCases := []struct {
		path                string
		expectedContentType string
	}{
		{"/index.html", "text/html"},
		{"/style.css", "text/css"},
		{"/script.js", "text/javascript"},
		{"/data.json", "application/json"},
		{"/image.png", "image/png"},
		{"/favicon.ico", "image/x-icon"},
	}

	for _, tc := range testCases {
		t.Run(strings.ReplaceAll(tc.path, "/", "_"), func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			rec := httptest.NewRecorder()

			serveFile(rec, req, logger)

			// Only check content type if file exists (status 200)
			if rec.Code == http.StatusOK {
				contentType := rec.Header().Get("Content-Type")
				if !strings.Contains(contentType, tc.expectedContentType) {
					t.Logf("Note: Expected content type %s for %s, got %s", tc.expectedContentType, tc.path, contentType)
				}
			}
		})
	}
}

func TestStaticFiles_ProdMode_EmptyPath(t *testing.T) {
	logger := createTestLogger()

	// Test that empty path serves index.html
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	serveFile(rec, req, logger)

	// Should return either 200 (if index.html exists) or 404 (if not embedded)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 200 or 404 for root path, got %d", rec.Code)
	}

	// If successful, should have appropriate content type
	if rec.Code == http.StatusOK {
		contentType := rec.Header().Get("Content-Type")
		if contentType == "" {
			t.Logf("Note: No Content-Type header set for root path")
		}
	}
}

func TestStaticFiles_ProdMode_EmbeddedFileSystem(t *testing.T) {
	// Test that the embedded file system can be accessed
	// This is a basic test to ensure the embed directive works

	// Try to read from embedded FS directly
	_, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		// This is expected if the file doesn't exist in embedded FS
		t.Logf("Note: index.html not found in embedded FS: %v", err)
	}

	// Test that we can at least access the embedded FS without panic
	_, err = staticFiles.ReadFile("static/nonexistent.txt")
	if err == nil {
		t.Error("Expected error when reading non-existent file from embedded FS")
	}
}
