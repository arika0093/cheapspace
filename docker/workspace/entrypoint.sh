#!/usr/bin/env bash
set -euo pipefail

# Ensure the workspace user exists and can access the Docker socket group.
if ! id -u codespace >/dev/null 2>&1; then
  useradd -m -s /bin/bash codespace
  echo "codespace ALL=(ALL) NOPASSWD:ALL" >/etc/sudoers.d/codespace
  chmod 0440 /etc/sudoers.d/codespace
fi
if getent group docker >/dev/null 2>&1; then
  usermod -aG docker codespace
fi

# Prepare the shared workspace and SSH key locations.
install -d -m 0700 -o codespace -g codespace /home/codespace/.ssh
install -d -m 0755 -o codespace -g codespace /workspaces
touch /home/codespace/.ssh/authorized_keys
chmod 0600 /home/codespace/.ssh/authorized_keys
chown codespace:codespace /home/codespace/.ssh/authorized_keys

# Populate authorized_keys from the provisioned environment when keys are supplied.
if [[ -n "${CHEAPSPACE_AUTHORIZED_KEYS:-}" ]]; then
  printf '%s\n' "${CHEAPSPACE_AUTHORIZED_KEYS}" >/home/codespace/.ssh/authorized_keys
  chown codespace:codespace /home/codespace/.ssh/authorized_keys
fi

# Set or lock the login password depending on the workspace access mode.
if [[ "${CHEAPSPACE_PASSWORD_AUTH_ENABLED:-false}" == "true" && -n "${CHEAPSPACE_PASSWORD:-}" ]]; then
  echo "codespace:${CHEAPSPACE_PASSWORD}" | chpasswd
else
  hidden_password="$(openssl rand -base64 24 | tr -d '\n')"
  echo "codespace:${hidden_password}" | chpasswd
fi

# Persist proxy and Docker environment variables for interactive login shells.
cat >/etc/profile.d/cheapspace-proxy.sh <<'EOF'
export DOCKER_HOST="unix:///var/run/docker.sock"
export HTTP_PROXY="${CHEAPSPACE_HTTP_PROXY:-}"
export HTTPS_PROXY="${CHEAPSPACE_HTTPS_PROXY:-}"
export NO_PROXY="${CHEAPSPACE_NO_PROXY:-}"
export PROXY_PAC_URL="${CHEAPSPACE_PROXY_PAC_URL:-}"
export http_proxy="${CHEAPSPACE_HTTP_PROXY:-}"
export https_proxy="${CHEAPSPACE_HTTPS_PROXY:-}"
export no_proxy="${CHEAPSPACE_NO_PROXY:-}"
export proxy_pac_url="${CHEAPSPACE_PROXY_PAC_URL:-}"
EOF

# Keep the proxy PAC URL in a predictable location for tools that want to read it directly.
if [[ -n "${CHEAPSPACE_PROXY_PAC_URL:-}" ]]; then
  install -d -m 0755 /etc/cheapspace
  printf '%s\n' "${CHEAPSPACE_PROXY_PAC_URL}" >/etc/cheapspace/proxy-pac-url
fi

# Clone the requested repository into /workspaces the first time the volume is prepared.
if [[ -n "${CHEAPSPACE_REPO_URL:-}" && ! -d /workspaces/.git ]]; then
  find /workspaces -mindepth 1 -maxdepth 1 -exec rm -rf {} +
  clone_args=(clone)
  if [[ -n "${CHEAPSPACE_REPO_BRANCH:-}" ]]; then
    clone_args+=(--branch "${CHEAPSPACE_REPO_BRANCH}")
  fi
  clone_args+=("${CHEAPSPACE_REPO_URL}" /workspaces)
  git "${clone_args[@]}"
  chown -R codespace:codespace /workspaces
fi

# Clone and optionally install user dotfiles once.
if [[ -n "${CHEAPSPACE_DOTFILES_URL:-}" && ! -d /home/codespace/.dotfiles ]]; then
  sudo -u codespace git clone "${CHEAPSPACE_DOTFILES_URL}" /home/codespace/.dotfiles
  if [[ -x /home/codespace/.dotfiles/install.sh ]]; then
    sudo -u codespace bash -lc '/home/codespace/.dotfiles/install.sh'
  fi
fi

# Start dockerd inside the workspace container so nested Docker commands work immediately.
if ! docker info >/dev/null 2>&1; then
  rm -f /var/run/docker.pid
  mkdir -p /var/lib/docker /var/run/docker
  nohup dockerd \
    --host=unix:///var/run/docker.sock \
    --data-root=/var/lib/docker \
    --exec-root=/var/run/docker \
    --pidfile=/var/run/docker.pid \
    --storage-driver=vfs \
    --iptables=false \
    --bridge=none \
    --ip-forward=false \
    --ip-masq=false \
    >/var/log/cheapspace-dockerd.log 2>&1 &

  for _ in $(seq 1 30); do
    if docker info >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done

  if ! docker info >/dev/null 2>&1; then
    echo "dockerd failed to start inside the workspace container" >&2
    cat /var/log/cheapspace-dockerd.log >&2 || true
    exit 1
  fi
fi

# Prepare the interactive shell experience and default tmux session for the codespace user.
sudo -u codespace bash -lc '
  git config --global --get-all safe.directory | grep -qx "/workspaces" || git config --global --add safe.directory /workspaces
  grep -q "cd /workspaces >/dev/null 2>&1 || true" "$HOME/.bashrc" || cat >>"$HOME/.bashrc" <<EOF
cd /workspaces >/dev/null 2>&1 || true
EOF
  if ! tmux has-session -t cheapspace 2>/dev/null; then
    tmux new-session -d -s cheapspace -c /workspaces
  fi
'

# Run the SSH daemon in the foreground as the main container process.
exec /usr/sbin/sshd -D -e
