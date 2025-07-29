//go:build debug
// +build debug

package webui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticFiles_DevMode_ServeFile(t *testing.T) {
	logger := createTestLogger()

	// Create temporary test files
	tempDir := t.TempDir()
	staticDir := filepath.Join(tempDir, "src", "webui", "static")
	err := os.MkdirAll(staticDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp static dir: %v", err)
	}

	// Create test files
	testFiles := map[string]string{
		"index.html":  "<html><body>Test Index</body></html>",
		"style.css":   "body { background: white; }",
		"script.js":   "console.log('test');",
		"image.png":   "fake png data",
		"data.json":   `{"test": "data"}`,
		"favicon.ico": "fake ico data",
	}

	for filename, content := range testFiles {
		filePath := filepath.Join(staticDir, filename)
		err := os.WriteFile(filePath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	// Change working directory temporarily
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(originalWd)

	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change working directory: %v", err)
	}

	// Test serveFile function
	testCases := []struct {
		path                string
		expectedStatus      int
		expectedContentType string
		expectedContent     string
	}{
		{"/", http.StatusOK, "text/html", testFiles["index.html"]},
		{"/index.html", http.StatusOK, "text/html", testFiles["index.html"]},
		{"/style.css", http.StatusOK, "text/css", testFiles["style.css"]},
		{"/script.js", http.StatusOK, "text/javascript", testFiles["script.js"]},
		{"/data.json", http.StatusOK, "application/json", testFiles["data.json"]},
		{"/nonexistent.txt", http.StatusNotFound, "", ""},
		{"/subdir/nonexistent.html", http.StatusNotFound, "", ""},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Path_%s", strings.ReplaceAll(tc.path, "/", "_")), func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			rec := httptest.NewRecorder()

			serveFile(rec, req, logger)

			if rec.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, rec.Code)
			}

			if tc.expectedStatus == http.StatusOK {
				contentType := rec.Header().Get("Content-Type")
				if tc.expectedContentType != "" && !strings.Contains(contentType, tc.expectedContentType) {
					t.Errorf("Expected content type to contain %s, got %s", tc.expectedContentType, contentType)
				}

				body := rec.Body.String()
				if body != tc.expectedContent {
					t.Errorf("Expected body %q, got %q", tc.expectedContent, body)
				}
			}
		})
	}
}

func TestStaticFiles_DevMode_SetupStaticHandler(t *testing.T) {
	// Create temporary test files for static handler testing
	tempDir := t.TempDir()
	staticDir := filepath.Join(tempDir, "src", "webui", "static")
	err := os.MkdirAll(staticDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp static dir: %v", err)
	}

	// Create test CSS file
	cssContent := "body { color: blue; }"
	cssPath := filepath.Join(staticDir, "test.css")
	err = os.WriteFile(cssPath, []byte(cssContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test CSS file: %v", err)
	}

	// Change working directory temporarily
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(originalWd)

	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change working directory: %v", err)
	}

	// Create HTTP server with static handler
	mux := http.NewServeMux()
	setupStaticHandler(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test static file serving
	resp, err := http.Get(server.URL + "/static/test.css")
	if err != nil {
		t.Fatalf("Error requesting static file: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}

	if string(body) != cssContent {
		t.Errorf("Expected CSS content %q, got %q", cssContent, string(body))
	}

	// Test Content-Type header
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/css") {
		t.Errorf("Expected Content-Type to contain text/css, got %s", contentType)
	}
}

func TestStaticFiles_DevMode_PathTraversal(t *testing.T) {
	logger := createTestLogger()

	// Create temporary test setup
	tempDir := t.TempDir()
	staticDir := filepath.Join(tempDir, "src", "webui", "static")
	err := os.MkdirAll(staticDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp static dir: %v", err)
	}

	// Create a sensitive file outside static directory
	sensitiveFile := filepath.Join(tempDir, "sensitive.txt")
	err = os.WriteFile(sensitiveFile, []byte("sensitive data"), 0644)
	if err != nil {
		t.Fatalf("Failed to create sensitive file: %v", err)
	}

	// Change working directory temporarily
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(originalWd)

	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change working directory: %v", err)
	}

	// Test path traversal attempts
	pathTraversalTests := []string{
		"/../sensitive.txt",
		"/../../sensitive.txt",
		"/../../../etc/passwd",
		"/..\\sensitive.txt",
		"/static/../../../sensitive.txt",
	}

	for _, path := range pathTraversalTests {
		t.Run(fmt.Sprintf("PathTraversal_%s", strings.ReplaceAll(path, "/", "_")), func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			rec := httptest.NewRecorder()

			serveFile(rec, req, logger)

			// Should return 404 for path traversal attempts
			if rec.Code != http.StatusNotFound {
				t.Errorf("Expected status 404 for path traversal attempt %s, got %d", path, rec.Code)
			}

			// Should not contain sensitive data
			body := rec.Body.String()
			if strings.Contains(body, "sensitive data") {
				t.Errorf("Path traversal successful for %s - sensitive data leaked", path)
			}
		})
	}
}

func TestStaticFiles_DevMode_EmptyPath(t *testing.T) {
	logger := createTestLogger()

	// Create temporary test setup with index.html
	tempDir := t.TempDir()
	staticDir := filepath.Join(tempDir, "src", "webui", "static")
	err := os.MkdirAll(staticDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp static dir: %v", err)
	}

	indexContent := "<html><body>Index Page</body></html>"
	indexPath := filepath.Join(staticDir, "index.html")
	err = os.WriteFile(indexPath, []byte(indexContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create index.html: %v", err)
	}

	// Change working directory temporarily
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(originalWd)

	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change working directory: %v", err)
	}

	// Test that empty path serves index.html
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	serveFile(rec, req, logger)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 for root path, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body != indexContent {
		t.Errorf("Expected index content %q, got %q", indexContent, body)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Expected Content-Type to contain text/html, got %s", contentType)
	}
}
