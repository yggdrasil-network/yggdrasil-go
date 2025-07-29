# WebUI Module

This module provides a web interface for managing Yggdrasil node through a browser.

## Features

- ✅ HTTP web server with static files
- ✅ Health check endpoint (`/health`)
- ✅ Development and production build modes
- ✅ Automatic binding to Yggdrasil IPv6 address
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
    "BindYgg": false
  }
}
```

### Configuration parameters:

- **`Enable`** - enable/disable WebUI
- **`Port`** - port for web interface (default 9000)
- **`Host`** - IP address to bind to (empty means all interfaces)
- **`BindYgg`** - automatically bind to Yggdrasil IPv6 address

## Usage

### Standard mode

```go
server := webui.Server("127.0.0.1:9000", logger)
```

### With core access

```go
server := webui.ServerWithCore("127.0.0.1:9000", logger, coreInstance)
```

### Automatic Yggdrasil address binding

```go
server := webui.ServerForYggdrasil(9000, logger, coreInstance)
// Automatically binds to [yggdrasil_ipv6]:9000
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

- **`/`** - main page (index.html)
- **`/health`** - health check (returns "OK")
- **`/static/*`** - static files (CSS, JS, images)

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