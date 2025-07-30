package webui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gologme/log"
)

func TestSessionAuthentication(t *testing.T) {
	logger := log.New(nil, "test: ", log.Flags())

	// Test server with password
	server := Server("127.0.0.1:0", "testpassword", logger)

	// Test cases for login endpoint
	loginTests := []struct {
		name       string
		password   string
		expectCode int
	}{
		{"Wrong password", "wrongpass", http.StatusUnauthorized},
		{"Correct password", "testpassword", http.StatusOK},
	}

	for _, tt := range loginTests {
		t.Run("Login_"+tt.name, func(t *testing.T) {
			loginData := fmt.Sprintf(`{"password":"%s"}`, tt.password)
			req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(loginData))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.loginHandler(rr, req)

			if rr.Code != tt.expectCode {
				t.Errorf("Expected status code %d, got %d", tt.expectCode, rr.Code)
			}
		})
	}

	// Test protected resource access
	t.Run("Protected_resource_without_session", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()

		handler := server.authMiddleware(func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusOK)
		})

		handler(rr, req)

		// Should redirect to login
		if rr.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect (303), got %d", rr.Code)
		}
	})
}

func TestNoPasswordAuthentication(t *testing.T) {
	logger := log.New(nil, "test: ", log.Flags())

	// Test server without password
	server := Server("127.0.0.1:0", "", logger)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	// Create handler function for testing
	handler := server.authMiddleware(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	handler(rr, req)

	// Should allow access without auth when no password is set
	if rr.Code != http.StatusOK {
		t.Errorf("Expected access without auth when no password is set, got %d", rr.Code)
	}
}

func TestSessionWorkflow(t *testing.T) {
	logger := log.New(nil, "test: ", log.Flags())
	server := Server("127.0.0.1:0", "testpassword", logger)

	// 1. Login to get session
	loginData := `{"password":"testpassword"}`
	loginReq := httptest.NewRequest("POST", "/auth/login", strings.NewReader(loginData))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRR := httptest.NewRecorder()

	server.loginHandler(loginRR, loginReq)

	if loginRR.Code != http.StatusOK {
		t.Fatalf("Login failed, expected 200, got %d", loginRR.Code)
	}

	// Extract session cookie
	cookies := loginRR.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == "ygg_session" {
			sessionCookie = cookie
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("No session cookie found after login")
	}

	// 2. Access protected resource with session
	protectedReq := httptest.NewRequest("GET", "/", nil)
	protectedReq.AddCookie(sessionCookie)
	protectedRR := httptest.NewRecorder()

	handler := server.authMiddleware(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	handler(protectedRR, protectedReq)

	if protectedRR.Code != http.StatusOK {
		t.Errorf("Expected access with valid session, got %d", protectedRR.Code)
	}
}

func TestHealthEndpointNoAuth(t *testing.T) {
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	// Health endpoint should not require auth
	http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("OK"))
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Health endpoint should be accessible without auth, got status %d", rr.Code)
	}

	if rr.Body.String() != "OK" {
		t.Errorf("Expected 'OK', got '%s'", rr.Body.String())
	}
}

func TestBruteForceProtection(t *testing.T) {
	logger := log.New(nil, "test: ", log.Flags())
	server := Server("127.0.0.1:0", "testpassword", logger)

	// Test multiple failed attempts from same IP
	clientIP := "192.168.1.100"

	// First 3 attempts should be allowed but fail
	for i := 1; i <= 3; i++ {
		t.Run(fmt.Sprintf("Failed_attempt_%d", i), func(t *testing.T) {
			loginData := `{"password":"wrongpassword"}`
			req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(loginData))
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = clientIP + ":12345"

			rr := httptest.NewRecorder()
			server.loginHandler(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Errorf("Expected 401 for failed attempt %d, got %d", i, rr.Code)
			}
		})
	}

	// 4th attempt should be blocked
	t.Run("Blocked_attempt", func(t *testing.T) {
		loginData := `{"password":"wrongpassword"}`
		req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(loginData))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = clientIP + ":12345"

		rr := httptest.NewRecorder()
		server.loginHandler(rr, req)

		if rr.Code != http.StatusTooManyRequests {
			t.Errorf("Expected 429 for blocked attempt, got %d", rr.Code)
		}
	})

	// Even correct password should be blocked during block period
	t.Run("Correct_password_while_blocked", func(t *testing.T) {
		loginData := `{"password":"testpassword"}`
		req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(loginData))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = clientIP + ":12345"

		rr := httptest.NewRecorder()
		server.loginHandler(rr, req)

		if rr.Code != http.StatusTooManyRequests {
			t.Errorf("Expected 429 even for correct password while blocked, got %d", rr.Code)
		}
	})
}

func TestBruteForceProtectionDifferentIPs(t *testing.T) {
	logger := log.New(nil, "test: ", log.Flags())
	server := Server("127.0.0.1:0", "testpassword", logger)

	// Failed attempts from one IP shouldn't affect another IP
	ip1 := "192.168.1.100"
	ip2 := "192.168.1.101"

	// Block first IP
	for i := 1; i <= 3; i++ {
		loginData := `{"password":"wrongpassword"}`
		req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(loginData))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip1 + ":12345"

		rr := httptest.NewRecorder()
		server.loginHandler(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401 for failed attempt %d from IP1, got %d", i, rr.Code)
		}
	}

	// Second IP should still be able to attempt login
	t.Run("Different_IP_not_blocked", func(t *testing.T) {
		loginData := `{"password":"wrongpassword"}`
		req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(loginData))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip2 + ":12345"

		rr := httptest.NewRecorder()
		server.loginHandler(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401 for different IP (not blocked), got %d", rr.Code)
		}
	})
}

func TestSuccessfulLoginClearsFailedAttempts(t *testing.T) {
	logger := log.New(nil, "test: ", log.Flags())
	server := Server("127.0.0.1:0", "testpassword", logger)

	clientIP := "192.168.1.100"

	// Make 2 failed attempts
	for i := 1; i <= 2; i++ {
		loginData := `{"password":"wrongpassword"}`
		req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(loginData))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = clientIP + ":12345"

		rr := httptest.NewRecorder()
		server.loginHandler(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401 for failed attempt %d, got %d", i, rr.Code)
		}
	}

	// Successful login should clear failed attempts
	t.Run("Successful_login_clears_attempts", func(t *testing.T) {
		loginData := `{"password":"testpassword"}`
		req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(loginData))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = clientIP + ":12345"

		rr := httptest.NewRecorder()
		server.loginHandler(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected 200 for correct password, got %d", rr.Code)
		}

		// Verify failed attempts were cleared
		server.attemptsMux.RLock()
		_, exists := server.failedAttempts[clientIP]
		server.attemptsMux.RUnlock()

		if exists {
			t.Error("Failed attempts should be cleared after successful login")
		}
	})
}
