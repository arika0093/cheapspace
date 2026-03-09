FROM mcr.microsoft.com/devcontainers/universal:latest

USER root

RUN apt-get update && apt-get install -y --no-install-recommends \
    docker.io \
    openssh-server \
    podman \
    tmux \
    uidmap \
    dbus-user-session \
    slirp4netns \
    fuse-overlayfs \
    sudo \
    curl \
    ca-certificates \
    git \
    && rm -rf /var/lib/apt/lists/*

RUN id -u codespace >/dev/null 2>&1 || useradd -m -s /bin/bash codespace
RUN npm install -g playwright @playwright/mcp @anthropic-ai/claude-code @openai/codex @github/copilot
RUN npx playwright install chromium

COPY docker/workspace/entrypoint.sh /usr/local/bin/cheapspace-workspace-entrypoint
RUN sed -i 's/\r$//' /usr/local/bin/cheapspace-workspace-entrypoint && chmod +x /usr/local/bin/cheapspace-workspace-entrypoint && mkdir -p /var/run/sshd /workspaces

EXPOSE 22
ENTRYPOINT ["/usr/local/bin/cheapspace-workspace-entrypoint"]

