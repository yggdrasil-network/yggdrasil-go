package webui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

type WebUIServer struct {
	server      *http.Server
	log         core.Logger
	listen      string
	password    string
	sessions    map[string]time.Time // sessionID -> expiry time
	sessionsMux sync.RWMutex
}

type LoginRequest struct {
	Password string `json:"password"`
}

func Server(listen string, password string, log core.Logger) *WebUIServer {
	return &WebUIServer{
		listen:   listen,
		password: password,
		log:      log,
		sessions: make(map[string]time.Time),
	}
}

// generateSessionID creates a random session ID
func (w *WebUIServer) generateSessionID() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// isValidSession checks if a session is valid and not expired
func (w *WebUIServer) isValidSession(sessionID string) bool {
	w.sessionsMux.RLock()
	defer w.sessionsMux.RUnlock()

	expiry, exists := w.sessions[sessionID]
	if !exists {
		return false
	}

	if time.Now().After(expiry) {
		// Session expired, clean it up
		go func() {
			w.sessionsMux.Lock()
			delete(w.sessions, sessionID)
			w.sessionsMux.Unlock()
		}()
		return false
	}

	return true
}

// createSession creates a new session for the user
func (w *WebUIServer) createSession() string {
	sessionID := w.generateSessionID()
	expiry := time.Now().Add(24 * time.Hour) // Session valid for 24 hours

	w.sessionsMux.Lock()
	w.sessions[sessionID] = expiry
	w.sessionsMux.Unlock()

	return sessionID
}

// cleanupExpiredSessions removes expired sessions
func (w *WebUIServer) cleanupExpiredSessions() {
	w.sessionsMux.Lock()
	defer w.sessionsMux.Unlock()

	now := time.Now()
	for sessionID, expiry := range w.sessions {
		if now.After(expiry) {
			delete(w.sessions, sessionID)
		}
	}
}

// authMiddleware checks for valid session or redirects to login
func (w *WebUIServer) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		// Skip authentication if no password is set
		if w.password == "" {
			next(rw, r)
			return
		}

		// Check for session cookie
		cookie, err := r.Cookie("ygg_session")
		if err != nil || !w.isValidSession(cookie.Value) {
			// No valid session - redirect to login page
			if r.URL.Path == "/login.html" || strings.HasPrefix(r.URL.Path, "/auth/") {
				// Allow access to login page and auth endpoints
				next(rw, r)
				return
			}

			// For API calls, return 401
			if strings.HasPrefix(r.URL.Path, "/api/") {
				rw.WriteHeader(http.StatusUnauthorized)
				return
			}

			// For regular pages, redirect to login
			http.Redirect(rw, r, "/login.html", http.StatusSeeOther)
			return
		}

		next(rw, r)
	}
}

// loginHandler handles password authentication
func (w *WebUIServer) loginHandler(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var loginReq LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&loginReq); err != nil {
		http.Error(rw, "Invalid request", http.StatusBadRequest)
		return
	}

	// Check password
	if subtle.ConstantTimeCompare([]byte(loginReq.Password), []byte(w.password)) != 1 {
		w.log.Debugf("Authentication failed for request from %s", r.RemoteAddr)
		http.Error(rw, "Invalid password", http.StatusUnauthorized)
		return
	}

	// Create session
	sessionID := w.createSession()

	// Set session cookie
	http.SetCookie(rw, &http.Cookie{
		Name:     "ygg_session",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil, // Only set Secure flag if using HTTPS
		SameSite: http.SameSiteStrictMode,
		MaxAge:   24 * 60 * 60, // 24 hours
	})

	w.log.Debugf("Successful authentication for request from %s", r.RemoteAddr)
	rw.WriteHeader(http.StatusOK)
}

// logoutHandler handles logout
func (w *WebUIServer) logoutHandler(rw http.ResponseWriter, r *http.Request) {
	// Get session cookie
	cookie, err := r.Cookie("ygg_session")
	if err == nil {
		// Remove session from server
		w.sessionsMux.Lock()
		delete(w.sessions, cookie.Value)
		w.sessionsMux.Unlock()
	}

	// Clear session cookie
	http.SetCookie(rw, &http.Cookie{
		Name:     "ygg_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1, // Delete cookie
	})

	// Redirect to login page
	http.Redirect(rw, r, "/login.html", http.StatusSeeOther)
}

func (w *WebUIServer) Start() error {
	// Validate listen address before starting
	if w.listen != "" {
		if _, _, err := net.SplitHostPort(w.listen); err != nil {
			return fmt.Errorf("invalid listen address: %v", err)
		}
	}

	// Start session cleanup routine
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			w.cleanupExpiredSessions()
		}
	}()

	mux := http.NewServeMux()

	// Authentication endpoints - no auth required
	mux.HandleFunc("/auth/login", w.loginHandler)
	mux.HandleFunc("/auth/logout", w.logoutHandler)

	// Setup static files handler (implementation varies by build)
	setupStaticHandler(mux, w)

	// Serve any file by path (implementation varies by build) - with auth
	mux.HandleFunc("/", w.authMiddleware(func(rw http.ResponseWriter, r *http.Request) {
		serveFile(rw, r, w.log)
	}))

	// Health check endpoint - no auth required
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("OK"))
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
