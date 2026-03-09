#!/usr/bin/env bash
set -euo pipefail

if ! id -u codespace >/dev/null 2>&1; then
  useradd -m -s /bin/bash codespace
  echo "codespace ALL=(ALL) NOPASSWD:ALL" >/etc/sudoers.d/codespace
  chmod 0440 /etc/sudoers.d/codespace
fi

install -d -m 0700 -o codespace -g codespace /home/codespace/.ssh
install -d -m 0755 -o codespace -g codespace /workspaces/project
touch /home/codespace/.ssh/authorized_keys
chmod 0600 /home/codespace/.ssh/authorized_keys
chown codespace:codespace /home/codespace/.ssh/authorized_keys

if [[ -n "${CHEAPSPACE_AUTHORIZED_KEYS:-}" ]]; then
  printf '%s\n' "${CHEAPSPACE_AUTHORIZED_KEYS}" >/home/codespace/.ssh/authorized_keys
  chown codespace:codespace /home/codespace/.ssh/authorized_keys
fi

sed -i 's/^#\?PasswordAuthentication .*/PasswordAuthentication no/' /etc/ssh/sshd_config
sed -i 's/^#\?PubkeyAuthentication .*/PubkeyAuthentication yes/' /etc/ssh/sshd_config
if [[ "${CHEAPSPACE_PASSWORD_AUTH_ENABLED:-false}" == "true" && -n "${CHEAPSPACE_PASSWORD:-}" ]]; then
  echo "codespace:${CHEAPSPACE_PASSWORD}" | chpasswd
  sed -i 's/^#\?PasswordAuthentication .*/PasswordAuthentication yes/' /etc/ssh/sshd_config
fi

cat >/etc/profile.d/cheapspace-proxy.sh <<'EOF'
export HTTP_PROXY="${CHEAPSPACE_HTTP_PROXY:-}"
export HTTPS_PROXY="${CHEAPSPACE_HTTPS_PROXY:-}"
export NO_PROXY="${CHEAPSPACE_NO_PROXY:-}"
export http_proxy="${CHEAPSPACE_HTTP_PROXY:-}"
export https_proxy="${CHEAPSPACE_HTTPS_PROXY:-}"
export no_proxy="${CHEAPSPACE_NO_PROXY:-}"
EOF

if [[ -n "${CHEAPSPACE_REPO_URL:-}" && ! -d /workspaces/project/.git ]]; then
  rm -rf /workspaces/project/*
  git clone --depth 1 "${CHEAPSPACE_REPO_URL}" /workspaces/project
  chown -R codespace:codespace /workspaces/project
fi

if [[ -n "${CHEAPSPACE_DOTFILES_URL:-}" && ! -d /home/codespace/.dotfiles ]]; then
  sudo -u codespace git clone --depth 1 "${CHEAPSPACE_DOTFILES_URL}" /home/codespace/.dotfiles
  if [[ -x /home/codespace/.dotfiles/install.sh ]]; then
    sudo -u codespace bash -lc '/home/codespace/.dotfiles/install.sh'
  fi
fi

sudo -u codespace bash -lc '
  export XDG_RUNTIME_DIR="$HOME/.docker/run"
  mkdir -p "$XDG_RUNTIME_DIR"
  if command -v dockerd-rootless.sh >/dev/null 2>&1; then
    nohup dockerd-rootless.sh >"$HOME/.docker-rootless.log" 2>&1 &
  elif command -v podman >/dev/null 2>&1; then
    nohup podman system service --time=0 "unix://${XDG_RUNTIME_DIR}/podman.sock" >"$HOME/.podman-service.log" 2>&1 &
    cat >"$HOME/.cheapspace-runtime.sh" <<EOF
export DOCKER_HOST="unix://${XDG_RUNTIME_DIR}/podman.sock"
alias docker=podman
EOF
    grep -q "cheapspace-runtime.sh" "$HOME/.bashrc" || echo "source \$HOME/.cheapspace-runtime.sh" >>"$HOME/.bashrc"
  fi
  if ! tmux has-session -t cheapspace 2>/dev/null; then
    tmux new-session -d -s cheapspace -c /workspaces/project
  fi
'

exec /usr/sbin/sshd -D -e

