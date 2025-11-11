# Development Environment Setup

This document describes how to set up a development environment for Yggdrasil using Docker and VS Code Dev Containers.

## Prerequisites

- Docker installed and running
- VS Code with the "Dev Containers" extension installed
- Git configured with your user information

## Option 1: VS Code Dev Containers (Recommended)

1. Open this project in VS Code
2. When prompted, click "Reopen in Container" or:
   - Press `Ctrl+Shift+P` (or `Cmd+Shift+P` on macOS)
   - Type "Dev Containers: Reopen in Container"
   - Select the option

VS Code will automatically build the development container and set up the environment with:
- Go 1.23 with all necessary tools
- Language server (gopls)
- Linting (golangci-lint)
- Debugging support (delve)
- Git integration
- Zsh shell with Oh My Zsh

## Option 2: Manual Docker Container

If you prefer to use Docker directly:

### Using Makefile commands:

```bash
# Build and run the development container
make -f Makefile dev

# Or build and run separately
make -f Makefile dev-build
make -f Makefile dev-run

# Get shell access to running container
make -f Makefile dev-shell

# Stop the container
make -f Makefile dev-stop

# Clean up
make -f Makefile dev-clean
```

### Using Docker directly:

```bash
# Build the development image
docker build -f Dockerfile -t yggdrasil-dev .

# Run the container
docker run -it --rm \
  --name yggdrasil-dev \
  -v $(pwd):/workspace \
  -v ~/.gitconfig:/home/vscode/.gitconfig:ro \
  -p 9000:9000 \
  -p 9001:9001 \
  -p 9002:9002 \
  -p 9003:9003 \
  --privileged \
  --cap-add=NET_ADMIN \
  --device=/dev/net/tun \
  yggdrasil-dev
```

## Development Features

The development environment includes:

- **Go Tools**: gopls, delve debugger, goimports, golangci-lint, staticcheck
- **Editor Support**: Syntax highlighting, auto-completion, debugging
- **Testing**: Go test runner and coverage tools
- **Networking**: Privileged access for network interface testing
- **Port Forwarding**: Ports 9000-9003 exposed for Yggdrasil services

## Building and Testing

Inside the container:

```bash
# Build the project
./build

# Run tests
go test ./...

# Generate configuration
./yggdrasil -genconf > yggdrasil.conf

# Run with configuration
./yggdrasil -useconf yggdrasil.conf
```

## Tips

1. Your local Git configuration is mounted into the container
2. The workspace directory (`/workspace`) is mapped to your local project directory
3. All changes made to source files are persistent on your host machine
4. The container runs as a non-root user (`vscode`) for security
5. Network capabilities are enabled for testing network-related features

## Troubleshooting

- If the container fails to start, ensure Docker has enough resources allocated
- For network-related issues, verify that the container has the necessary privileges
- If Go tools are missing, rebuild the container: `make -f Makefile dev-clean dev-build` 