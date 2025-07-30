# WebUI Module

This module provides a web interface for managing Yggdrasil node through a browser.

## Features

- ✅ HTTP web server with static files
- ✅ Health check endpoint (`/health`)
- ✅ Development and production build modes
- ✅ Custom session-based authentication
- ✅ Beautiful login page (password-only)
- ✅ Session management with automatic cleanup
- ✅ IPv4 and IPv6 support
- ✅ Path traversal attack protection

## Configuration

In the Yggdrasil configuration file:

```json
{
  "WebUI": {
    "Enable": true,
    "Port": 9000,
    "Host": "",
    "Password": "your_secure_password"
  }
}
```

### Configuration parameters:

- **`Enable`** - enable/disable WebUI
- **`Port`** - port for web interface (default 9000)
- **`Host`** - IP address to bind to (empty means all interfaces)
- **`Password`** - password for accessing the web interface (optional, if empty no authentication required)

## Usage

### Without password authentication

```go
server := webui.Server("127.0.0.1:9000", "", logger)
```

### With password authentication

```go
server := webui.Server("127.0.0.1:9000", "your_password", logger)
```

### Starting the server

```go
go func() {
    if err := server.Start(); err != nil {
        logger.Errorf("WebUI server error: %v", err)
    }
}()

// To stop
server.Stop()
```

## Endpoints

- **`/`** - main page (index.html) - requires authentication if password is set
- **`/login.html`** - custom login page (only password required)
- **`/auth/login`** - POST endpoint for authentication
- **`/auth/logout`** - logout endpoint (clears session)
- **`/health`** - health check (returns "OK") - no authentication required
- **`/static/*`** - static files (CSS, JS, images) - requires authentication if password is set

## Build modes

### Development mode (`-tags debug`)
- Files loaded from disk from `src/webui/static/`
- File changes available without rebuild

### Production mode (default)
- Files embedded in binary
- Faster loading, smaller deployment size

## Security

- Path traversal attack protection
- Configured HTTP timeouts
- Header size limits
- File MIME type validation
- Custom session-based authentication (password protection)
- HttpOnly and Secure cookies
- Session expiration (24 hours)
- Health check endpoint always accessible without authentication

## Testing

The module includes a comprehensive test suite:

```bash
cd src/webui
go test -v
```

Tests cover:
- Server creation and management
- HTTP endpoints
- Static files (dev and prod modes)
- Error handling
- Configuration
- Yggdrasil IPv6 binding 