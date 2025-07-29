package webui

import (
	"fmt"
	"net/http"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

type WebUIServer struct {
	server *http.Server
	log    core.Logger
	listen string
}

func Server(listen string, log core.Logger) *WebUIServer {
	return &WebUIServer{
		listen: listen,
		log:    log,
	}
}

func (w *WebUIServer) Start() error {
	mux := http.NewServeMux()

	// Setup static files handler (implementation varies by build)
	setupStaticHandler(mux)

	// Serve any file by path (implementation varies by build)
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		serveFile(rw, r, w.log)
	})

	// Health check endpoint
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte("OK"))
	})

	w.server = &http.Server{
		Addr:           w.listen,
		Handler:        mux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	w.log.Infof("WebUI server starting on %s", w.listen)

	if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("WebUI server failed: %v", err)
	}

	return nil
}

func (w *WebUIServer) Stop() error {
	if w.server != nil {
		return w.server.Close()
	}
	return nil
}
