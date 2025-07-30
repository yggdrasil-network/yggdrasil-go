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

	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

type WebUIServer struct {
	server         *http.Server
	log            core.Logger
	listen         string
	password       string
	sessions       map[string]time.Time // sessionID -> expiry time
	sessionsMux    sync.RWMutex
	failedAttempts map[string]*FailedLoginInfo // IP -> failed login info
	attemptsMux    sync.RWMutex
	admin          *admin.AdminSocket // Admin socket reference for direct API calls
}

type LoginRequest struct {
	Password string `json:"password"`
}

type FailedLoginInfo struct {
	Count        int
	LastAttempt  time.Time
	BlockedUntil time.Time
}

const (
	MaxFailedAttempts = 3
	BlockDuration     = 1 * time.Minute
	AttemptWindow     = 15 * time.Minute // Reset counter if no attempts in 15 minutes
)

func Server(listen string, password string, log core.Logger) *WebUIServer {
	return &WebUIServer{
		listen:         listen,
		password:       password,
		log:            log,
		sessions:       make(map[string]time.Time),
		failedAttempts: make(map[string]*FailedLoginInfo),
		admin:          nil, // Will be set later via SetAdmin
	}
}

// SetAdmin sets the admin socket reference for direct API calls
func (w *WebUIServer) SetAdmin(admin *admin.AdminSocket) {
	w.admin = admin
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

// getClientIP extracts the real client IP from request
func (w *WebUIServer) getClientIP(r *http.Request) string {
	// Check for forwarded IP headers (for reverse proxies)
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// Take the first IP in the chain
		ips := strings.Split(forwarded, ",")
		return strings.TrimSpace(ips[0])
	}

	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Extract IP from RemoteAddr
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

// isIPBlocked checks if an IP address is currently blocked
func (w *WebUIServer) isIPBlocked(ip string) bool {
	w.attemptsMux.RLock()
	defer w.attemptsMux.RUnlock()

	info, exists := w.failedAttempts[ip]
	if !exists {
		return false
	}

	return time.Now().Before(info.BlockedUntil)
}

// recordFailedAttempt records a failed login attempt for an IP
func (w *WebUIServer) recordFailedAttempt(ip string) {
	w.attemptsMux.Lock()
	defer w.attemptsMux.Unlock()

	now := time.Now()
	info, exists := w.failedAttempts[ip]

	if !exists {
		info = &FailedLoginInfo{}
		w.failedAttempts[ip] = info
	}

	// Reset counter if last attempt was too long ago
	if now.Sub(info.LastAttempt) > AttemptWindow {
		info.Count = 0
	}

	info.Count++
	info.LastAttempt = now

	// Block IP if too many failed attempts
	if info.Count >= MaxFailedAttempts {
		info.BlockedUntil = now.Add(BlockDuration)
		w.log.Warnf("IP %s blocked for %v after %d failed login attempts", ip, BlockDuration, info.Count)
	}
}

// clearFailedAttempts clears failed attempts for an IP (on successful login)
func (w *WebUIServer) clearFailedAttempts(ip string) {
	w.attemptsMux.Lock()
	defer w.attemptsMux.Unlock()

	delete(w.failedAttempts, ip)
}

// cleanupFailedAttempts removes old failed attempt records
func (w *WebUIServer) cleanupFailedAttempts() {
	w.attemptsMux.Lock()
	defer w.attemptsMux.Unlock()

	now := time.Now()
	for ip, info := range w.failedAttempts {
		// Remove if block period has expired and no recent attempts
		if now.After(info.BlockedUntil) && now.Sub(info.LastAttempt) > AttemptWindow {
			delete(w.failedAttempts, ip)
		}
	}
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

// loginHandler handles password authentication with brute force protection
func (w *WebUIServer) loginHandler(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := w.getClientIP(r)

	// Check if IP is blocked
	if w.isIPBlocked(clientIP) {
		w.log.Warnf("Blocked login attempt from %s (IP is temporarily blocked)", clientIP)
		http.Error(rw, "Too many failed attempts. Please try again later.", http.StatusTooManyRequests)
		return
	}

	var loginReq LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&loginReq); err != nil {
		http.Error(rw, "Invalid request", http.StatusBadRequest)
		return
	}

	// Check password
	if subtle.ConstantTimeCompare([]byte(loginReq.Password), []byte(w.password)) != 1 {
		w.log.Debugf("Authentication failed for request from %s", clientIP)
		w.recordFailedAttempt(clientIP)
		http.Error(rw, "Invalid password", http.StatusUnauthorized)
		return
	}

	// Successful login - clear any failed attempts
	w.clearFailedAttempts(clientIP)

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

	w.log.Infof("Successful authentication for IP %s", clientIP)
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

// adminAPIHandler handles direct admin API calls
func (w *WebUIServer) adminAPIHandler(rw http.ResponseWriter, r *http.Request) {
	if w.admin == nil {
		http.Error(rw, "Admin API not available", http.StatusServiceUnavailable)
		return
	}

	// Extract command from URL path
	// /api/admin/getSelf -> getSelf
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/")
	command := strings.Split(path, "/")[0]

	if command == "" {
		// Return list of available commands
		commands := w.admin.GetAvailableCommands()
		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(map[string]interface{}{
			"status":   "success",
			"commands": commands,
		})
		return
	}

	var args map[string]interface{}
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			args = make(map[string]interface{})
		}
	} else {
		args = make(map[string]interface{})
	}

	// Call admin handler directly
	result, err := w.callAdminHandler(command, args)
	if err != nil {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(rw).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]interface{}{
		"status":   "success",
		"response": result,
	})
}

// callAdminHandler calls admin handlers directly without socket
func (w *WebUIServer) callAdminHandler(command string, args map[string]interface{}) (interface{}, error) {
	argsBytes, err := json.Marshal(args)
	if err != nil {
		argsBytes = []byte("{}")
	}

	return w.admin.CallHandler(command, argsBytes)
}

func (w *WebUIServer) Start() error {
	// Validate listen address before starting
	if w.listen != "" {
		if _, _, err := net.SplitHostPort(w.listen); err != nil {
			return fmt.Errorf("invalid listen address: %v", err)
		}
	}

	// Start cleanup routines
	go func() {
		sessionTicker := time.NewTicker(1 * time.Hour)
		attemptsTicker := time.NewTicker(5 * time.Minute) // Clean failed attempts more frequently
		defer sessionTicker.Stop()
		defer attemptsTicker.Stop()

		for {
			select {
			case <-sessionTicker.C:
				w.cleanupExpiredSessions()
			case <-attemptsTicker.C:
				w.cleanupFailedAttempts()
			}
		}
	}()

	mux := http.NewServeMux()

	// Authentication endpoints - no auth required
	mux.HandleFunc("/auth/login", w.loginHandler)
	mux.HandleFunc("/auth/logout", w.logoutHandler)

	// Admin API endpoints - with auth
	mux.HandleFunc("/api/admin/", w.authMiddleware(w.adminAPIHandler))

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
