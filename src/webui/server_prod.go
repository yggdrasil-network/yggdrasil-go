//go:build !debug
// +build !debug

package webui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

//go:embed static/*
var staticFiles embed.FS

// setupStaticHandler configures static file serving for production (embedded files)
func setupStaticHandler(mux *http.ServeMux) {
	// Get the embedded file system for static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("failed to get embedded static files: " + err.Error())
	}

	// Serve static files from embedded FS
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
}

// serveFile serves any file from embedded files or returns 404 if not found
func serveFile(rw http.ResponseWriter, r *http.Request, log core.Logger) {
	// Clean the path and remove leading slash
	requestPath := strings.TrimPrefix(r.URL.Path, "/")

	// If path is empty, serve index.html
	if requestPath == "" {
		requestPath = "index.html"
	}

	// Construct the full path within static directory
	filePath := "static/" + requestPath

	// Try to read the file from embedded FS
	data, err := staticFiles.ReadFile(filePath)
	if err != nil {
		log.Debugf("File not found: %s", filePath)
		http.NotFound(rw, r)
		return
	}

	// Determine content type based on file extension
	contentType := mime.TypeByExtension(filepath.Ext(requestPath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Set headers and serve the file
	rw.Header().Set("Content-Type", contentType)
	rw.Write(data)

	log.Debugf("Served file: %s (type: %s)", filePath, contentType)
}
