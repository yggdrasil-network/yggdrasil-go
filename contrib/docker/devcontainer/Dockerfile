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

# Install Oh My Zsh for better terminal experience
RUN sh -c "ZSH=/usr/local/share/zsh/oh-my-zsh $(curl -fsSL https://raw.github.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" --unattended \
    && ZSH_CUSTOM=/usr/local/share/zsh/oh-my-zsh/custom \
    && git clone https://github.com/chrissicool/zsh-256color $ZSH_CUSTOM/plugins/zsh-256color \
    && git clone https://github.com/zsh-users/zsh-autosuggestions $ZSH_CUSTOM/plugins/zsh-autosuggestions \
    && git clone https://github.com/popstas/zsh-command-time.git $ZSH_CUSTOM/plugins/command-time

# Setup zshrc
COPY contrib/docker/devcontainer/zshrc /usr/local/share/zsh/.zshrc
RUN rm -f /root/.zshrc \
    && ln -s /usr/local/share/zsh/.zshrc /root/.zshrc \
    && ln -s /usr/local/share/zsh/.zshrc /home/vscode/.zshrc

# Set up shell environment for vscode user
USER $USERNAME

# Set up shell environment for vscode user
ENV SHELL=/bin/zsh

# Expose common ports that might be used by Yggdrasil
EXPOSE 9001 9002 9003

# Keep container running for dev containers
CMD ["sleep", "infinity"] 