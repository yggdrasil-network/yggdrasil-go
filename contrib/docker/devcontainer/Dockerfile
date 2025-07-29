# Development Dockerfile for VS Code Dev Containers
FROM golang:1.24-bullseye

# Install only essential system packages
RUN apt-get update && apt-get install -y \
    git \
    ca-certificates \
    sudo \
    zsh \
    locales \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Install Oh My Zsh for better terminal experience
RUN sh -c "$(curl -fsSL https://raw.github.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" --unattended

# Install essential Go development tools only
RUN go install golang.org/x/tools/gopls@latest \
    && go install github.com/go-delve/delve/cmd/dlv@latest \
    && go install golang.org/x/tools/cmd/goimports@latest \
    && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Create a non-root user for development
ARG USERNAME=vscode
ARG USER_UID=1000
ARG USER_GID=$USER_UID

# Create user and group with proper permissions
RUN groupadd --gid $USER_GID $USERNAME \
    && useradd --uid $USER_UID --gid $USER_GID -m $USERNAME -s /bin/zsh \
    && echo $USERNAME ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/$USERNAME \
    && chmod 0440 /etc/sudoers.d/$USERNAME

# Fix Go module cache permissions
RUN chown -R $USERNAME:$USERNAME /go/pkg/mod || true \
    && chmod -R 755 /go/pkg/mod || true

# Create default yggdrasil conf directory
RUN mkdir -p /etc/yggdrasil \
    && chown -R $USERNAME:$USERNAME /etc/yggdrasil

# Set up the workspace with proper ownership
WORKDIR /workspace
RUN chown $USERNAME:$USERNAME /workspace

# Copy go module files to enable dependency caching
COPY go.mod go.sum ./
RUN go mod download && chown -R $USERNAME:$USERNAME /workspace

# Set up shell environment for vscode user
USER $USERNAME
ENV SHELL=/bin/zsh
RUN echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.zshrc \
    && echo 'export GO111MODULE=on' >> ~/.zshrc \
    && echo 'export LANG=C.UTF-8' >> ~/.zshrc \
    && echo 'export LC_ALL=C.UTF-8' >> ~/.zshrc \
    && echo 'export LC_CTYPE=C.UTF-8' >> ~/.zshrc \
    && mkdir -p ~/.cache

# Expose common ports that might be used by Yggdrasil
EXPOSE 9001 9002 9003

# Keep container running for dev containers
CMD ["sleep", "infinity"] 