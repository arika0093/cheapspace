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
    git

RUN id -u codespace >/dev/null 2>&1 || useradd -m -s /bin/bash codespace
RUN usermod -aG docker codespace || true
RUN npm install -g playwright @playwright/mcp @anthropic-ai/claude-code @openai/codex @github/copilot
RUN npx playwright install chromium

RUN install -d -m 0755 /var/run/sshd /workspaces \
    && set_sshd_option() { key="$1"; value="$2"; if grep -q "^#\?${key} " /etc/ssh/sshd_config; then sed -i "s/^#\?${key} .*/${key} ${value}/" /etc/ssh/sshd_config; else echo "${key} ${value}" >> /etc/ssh/sshd_config; fi; } \
    && set_sshd_option Port 2222 \
    && set_sshd_option PasswordAuthentication yes \
    && set_sshd_option PubkeyAuthentication yes \
    && set_sshd_option PermitRootLogin no \
    && (grep -q '^AllowUsers codespace$' /etc/ssh/sshd_config || echo 'AllowUsers codespace' >> /etc/ssh/sshd_config)

COPY docker/workspace/entrypoint.sh /usr/local/bin/cheapspace-workspace-entrypoint
RUN sed -i 's/\r$//' /usr/local/bin/cheapspace-workspace-entrypoint && chmod +x /usr/local/bin/cheapspace-workspace-entrypoint

EXPOSE 2222
ENTRYPOINT ["/usr/local/bin/cheapspace-workspace-entrypoint"]

