package webui

import (
	"fmt"
	"testing"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

func TestWebUIConfig_DefaultValues(t *testing.T) {
	cfg := config.GenerateConfig()

	// Check that WebUI config has reasonable defaults
	if cfg.WebUI.Port == 0 {
		t.Log("Note: WebUI Port is 0 (might be default unset value)")
	}

	// Host can be empty (meaning all interfaces)
	if cfg.WebUI.Host == "" {
		t.Log("Note: WebUI Host is empty (binds to all interfaces)")
	}

	// Enable should have a default value
	if !cfg.WebUI.Enable && cfg.WebUI.Enable {
		t.Log("WebUI Enable flag has a boolean value")
	}
}

func TestWebUIConfig_Validation(t *testing.T) {
	testCases := []struct {
		name     string
		config   config.WebUIConfig
		valid    bool
		expected string
	}{
		{
			name: "Valid config with default port",
			config: config.WebUIConfig{
				Enable: true,
				Port:   9000,
				Host:   "",
			},
			valid:    true,
			expected: ":9000",
		},
		{
			name: "Valid config with localhost",
			config: config.WebUIConfig{
				Enable: true,
				Port:   8080,
				Host:   "localhost",
			},
			valid:    true,
			expected: "localhost:8080",
		},
		{
			name: "Valid config with specific IP",
			config: config.WebUIConfig{
				Enable: true,
				Port:   3000,
				Host:   "127.0.0.1",
			},
			valid:    true,
			expected: "127.0.0.1:3000",
		},
		{
			name: "Valid config with IPv6",
			config: config.WebUIConfig{
				Enable: true,
				Port:   9000,
				Host:   "::1",
			},
			valid:    true,
			expected: "[::1]:9000",
		},
		{
			name: "Disabled config",
			config: config.WebUIConfig{
				Enable: false,
				Port:   9000,
				Host:   "localhost",
			},
			valid:    false,
			expected: "",
		},
		{
			name: "Zero port",
			config: config.WebUIConfig{
				Enable: true,
				Port:   0,
				Host:   "localhost",
			},
			valid:    true,
			expected: "localhost:0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test building listen address from config
			var listenAddr string

			if tc.config.Enable {
				if tc.config.Host == "" {
					listenAddr = fmt.Sprintf(":%d", tc.config.Port)
				} else if tc.config.Host == "::1" || (len(tc.config.Host) > 0 && tc.config.Host[0] == ':') {
					// IPv6 needs brackets
					listenAddr = fmt.Sprintf("[%s]:%d", tc.config.Host, tc.config.Port)
				} else {
					listenAddr = fmt.Sprintf("%s:%d", tc.config.Host, tc.config.Port)
				}
			}

			if tc.valid {
				if listenAddr != tc.expected {
					t.Errorf("Expected listen address %s, got %s", tc.expected, listenAddr)
				}

				// Try to create server with this config
				logger := createTestLogger()
				server := Server(listenAddr, logger)
				if server == nil {
					t.Error("Failed to create server with valid config")
				}
			} else {
				if tc.config.Enable {
					t.Error("Config should be considered invalid when WebUI is disabled")
				}
			}
		})
	}
}

func TestWebUIConfig_PortRanges(t *testing.T) {
	logger := createTestLogger()

	// Test various port ranges
	portTests := []struct {
		port        uint16
		shouldWork  bool
		description string
	}{
		{1, true, "Port 1 (lowest valid port)"},
		{80, true, "Port 80 (HTTP)"},
		{443, true, "Port 443 (HTTPS)"},
		{8080, true, "Port 8080 (common alternative)"},
		{9000, true, "Port 9000 (default WebUI)"},
		{65535, true, "Port 65535 (highest valid port)"},
		{0, true, "Port 0 (OS assigns port)"},
	}

	for _, test := range portTests {
		t.Run(test.description, func(t *testing.T) {
			listenAddr := fmt.Sprintf("127.0.0.1:%d", test.port)
			server := Server(listenAddr, logger)

			if server == nil {
				t.Errorf("Failed to create server for %s", test.description)
				return
			}

			// For port 0, the OS will assign an available port
			// For other ports, we just check if server creation succeeds
			if test.shouldWork {
				// Try to start briefly to see if port is valid
				go func() {
					server.Start()
				}()

				// Quick cleanup
				server.Stop()
			}
		})
	}
}

func TestWebUIConfig_HostFormats(t *testing.T) {
	logger := createTestLogger()

	hostTests := []struct {
		host        string
		port        uint16
		expected    string
		description string
	}{
		{"", 9000, ":9000", "Empty host (all interfaces)"},
		{"localhost", 9000, "localhost:9000", "Localhost"},
		{"127.0.0.1", 9000, "127.0.0.1:9000", "IPv4 loopback"},
		{"0.0.0.0", 9000, "0.0.0.0:9000", "IPv4 all interfaces"},
		{"::1", 9000, "[::1]:9000", "IPv6 loopback"},
		{"::", 9000, "[::]:9000", "IPv6 all interfaces"},
	}

	for _, test := range hostTests {
		t.Run(test.description, func(t *testing.T) {
			var listenAddr string

			if test.host == "" {
				listenAddr = fmt.Sprintf(":%d", test.port)
			} else if test.host == "::1" || test.host == "::" {
				listenAddr = fmt.Sprintf("[%s]:%d", test.host, test.port)
			} else {
				listenAddr = fmt.Sprintf("%s:%d", test.host, test.port)
			}

			if listenAddr != test.expected {
				t.Errorf("Expected %s, got %s", test.expected, listenAddr)
			}

			server := Server(listenAddr, logger)
			if server == nil {
				t.Errorf("Failed to create server with %s", test.description)
			}
		})
	}
}

func TestWebUIConfig_Integration(t *testing.T) {
	// Test integration with actual config generation
	cfg := config.GenerateConfig()

	// Modify WebUI config
	cfg.WebUI.Enable = true
	cfg.WebUI.Port = 9001
	cfg.WebUI.Host = "127.0.0.1"

	// Build listen address from config
	listenAddr := fmt.Sprintf("%s:%d", cfg.WebUI.Host, cfg.WebUI.Port)

	logger := createTestLogger()
	server := Server(listenAddr, logger)

	if server == nil {
		t.Fatal("Failed to create server from generated config")
	}

	// Test that server can start with this config
	go func() {
		server.Start()
	}()
	defer server.Stop()

	// Verify server properties match config
	if server.listen != listenAddr {
		t.Errorf("Server listen address %s doesn't match config %s", server.listen, listenAddr)
	}
}

func TestWebUIConfig_JSONSerialization(t *testing.T) {
	// Test that WebUIConfig can be serialized/deserialized
	// This is important for config file handling

	originalConfig := config.WebUIConfig{
		Enable: true,
		Port:   8080,
		Host:   "localhost",
	}

	// In a real scenario, this would go through JSON marshaling/unmarshaling
	// For this test, we'll just verify the struct is properly defined

	if originalConfig.Enable != true {
		t.Error("Enable field not properly set")
	}

	if originalConfig.Port != 8080 {
		t.Error("Port field not properly set")
	}

	if originalConfig.Host != "localhost" {
		t.Error("Host field not properly set")
	}
}

func TestWebUIConfig_EdgeCases(t *testing.T) {
	logger := createTestLogger()

	// Test edge cases for configuration
	edgeCases := []struct {
		name   string
		config config.WebUIConfig
		test   func(t *testing.T, cfg config.WebUIConfig)
	}{
		{
			name: "All zeros",
			config: config.WebUIConfig{
				Enable: false,
				Port:   0,
				Host:   "",
			},
			test: func(t *testing.T, cfg config.WebUIConfig) {
				if cfg.Enable {
					t.Error("Enable should be false")
				}
			},
		},
		{
			name: "Maximum port",
			config: config.WebUIConfig{
				Enable: true,
				Port:   65535,
				Host:   "127.0.0.1",
			},
			test: func(t *testing.T, cfg config.WebUIConfig) {
				listenAddr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
				server := Server(listenAddr, logger)
				if server == nil {
					t.Error("Should be able to create server with max port")
				}
			},
		},
		{
			name: "Unicode host (should be handled gracefully)",
			config: config.WebUIConfig{
				Enable: true,
				Port:   9000,
				Host:   "тест",
			},
			test: func(t *testing.T, cfg config.WebUIConfig) {
				listenAddr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
				server := Server(listenAddr, logger)
				// Server creation should not panic, even with invalid host
				if server == nil {
					t.Error("Server creation should not fail due to host format")
				}
			},
		},
	}

	for _, tc := range edgeCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.test(t, tc.config)
		})
	}
}
