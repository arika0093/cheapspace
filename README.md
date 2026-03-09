# Cheapspace

Alternative to GitHub Codespaces. Self-hostable.

## What is "*cheap*"?

* *not expensive to use*: It's self-hostable, so it doesn't cost you anything.
* *low quality*: It has only simple features. git, docker, sshd.
* *easy to use*: Just start it up with docker-compose. It's simple.

## Features

- Single-binary Go control plane with SQLite persistence.
- Workspace list, detail, create, and delete flows.
- CLI workspace list/view/new/delete commands.
- Job center with list/detail views, log streaming, stop/resume/delete actions.
- Built-in image, direct image reference, manual Dockerfile, and Nixpacks source modes.
- Repository URL plus optional branch selection.
- Multiple SSH public keys plus password-login fallback with one-time reveal.
- Light/dark UI toggle with a default light theme and dense dashboard layout.
- Nested Docker workspace image with SSH, tmux, repo clone, and proxy bootstrapping.
- Playwright smoke E2E coverage using the mock runtime.

## Installation

### Local development

```powershell
npm install
npm run build:css
go test ./...
go run .\cmd\cheapspace serve
```

The server defaults to `CHEAPSPACE_RUNTIME=mock`, which is the recommended mode for local development, UI work, and Playwright smoke tests.

### Local end-to-end verification

```powershell
npm install
npm run build:css
npm run test:e2e
```

### Docker Compose

`docker compose up --build` starts the Cheapspace app as a single service and mounts `/var/run/docker.sock` so the app can manage sibling workspace containers.

```powershell
docker build -f docker/workspace/Dockerfile -t ghcr.io/arika0093/cheapspace-workspace:latest .
docker compose up --build
```

Open `http://localhost:8080/workspaces`.

The compose file also defines `CHEAPSPACE_PORT_RANGE_START` / `CHEAPSPACE_PORT_RANGE_END`, which drive the allowed SSH host-port range shown in the UI.
Set `CHEAPSPACE_HTTP_PORT` before `docker compose up` when port `8080` is already in use locally.

### Migrations

Cheapspace ships with a built-in migration utility.

```powershell
go run .\cmd\cheapspace migrate up
go run .\cmd\cheapspace migrate status
```

### CLI workspace management

Cheapspace also exposes local CLI operations against the same SQLite database used by the server.

```powershell
go run .\cmd\cheapspace workspace list
go run .\cmd\cheapspace workspace new --name local-dev --repo-url https://github.com/arika0093/console2svg --repo-branch main --source-type builtin_image --cpu-cores 2 --memory-mb 4096
go run .\cmd\cheapspace workspace view ws-xxxxxxxxxxxx
go run .\cmd\cheapspace workspace delete ws-xxxxxxxxxxxx
```

## Runtime notes

- The local mock runtime is the default developer experience and powers the Playwright smoke test.
- The Docker runtime is enabled in `docker-compose.yml` and expects a mounted Docker-compatible socket.
- `CHEAPSPACE_DEFAULT_WORKSPACE_IMAGE` defaults to the public GHCR image `ghcr.io/arika0093/cheapspace-workspace:latest`, so end users can pull it without `docker login`.
- `docker-compose.yml` now allows overriding that image with `CHEAPSPACE_DEFAULT_WORKSPACE_IMAGE` for local validation.
- Native SSH access is exposed by host port mapping in phase 1.
- Traefik/sslip.io hostname generation is optional metadata for future ingress integration; raw SSH over shared port 443 remains a phase-2 problem.
- The workspace image is defined in `docker\workspace\Dockerfile`, listens on container port `2222`, clones repositories into `/workspaces`, starts nested `dockerd` during container boot, and configures tmux, dotfiles, and Playwright tooling.
