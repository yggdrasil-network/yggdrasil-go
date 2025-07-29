//go:build debug
// +build debug

package webui

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// setupStaticHandler configures static file serving for development (files from disk)
func setupStaticHandler(mux *http.ServeMux) {
	// Serve static files from disk for development
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("src/webui/static/"))))
}

// serveFile serves any file from disk or returns 404 if not found
func serveFile(rw http.ResponseWriter, r *http.Request, log core.Logger) {
	// Clean the path and remove leading slash
	requestPath := strings.TrimPrefix(r.URL.Path, "/")

	// If path is empty, serve index.html
	if requestPath == "" {
		requestPath = "index.html"
	}

	// Construct the full path on disk
	filePath := filepath.Join("src/webui/static", requestPath)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Debugf("File not found: %s", filePath)
		http.NotFound(rw, r)
		return
	}

	// Determine content type based on file extension
	contentType := mime.TypeByExtension(filepath.Ext(requestPath))
	if contentType != "" {
		rw.Header().Set("Content-Type", contentType)
	}

	// Serve the file
	log.Debugf("Serving file from disk: %s", filePath)
	http.ServeFile(rw, r, filePath)
}
