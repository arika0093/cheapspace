# Cheapspace

Alternative to GitHub Codespaces. Self-hostable.

## What is "*cheap*"?

* *not expensive to use*: It's self-hostable, so it doesn't cost you anything.
* *low quality*: It has only simple features. git, docker, sshd.
* *easy to use*: Just start it up with docker-compose. It's simple.

## Features

- Single-binary Go control plane with SQLite persistence.
- Workspace list, detail, create, and delete flows.
- Job center with list/detail views, log streaming, stop/resume/delete actions.
- Built-in image, direct image reference, manual Dockerfile, and Nixpacks source modes.
- Multiple SSH public keys plus password-login fallback with one-time reveal.
- Rootless DinD-oriented workspace image design.
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
docker build -f docker/workspace/Dockerfile -t cheapspace-workspace:latest .
docker compose up --build
```

Open `http://localhost:8080/workspaces`.

### Migrations

Cheapspace ships with a built-in migration utility.

```powershell
go run .\cmd\cheapspace migrate up
go run .\cmd\cheapspace migrate status
```

## Runtime notes

- The local mock runtime is the default developer experience and powers the Playwright smoke test.
- The Docker runtime is enabled in `docker-compose.yml` and expects a mounted Docker-compatible socket.
- `CHEAPSPACE_DEFAULT_WORKSPACE_IMAGE` defaults to the local tag `cheapspace-workspace:latest`; override it if you publish the workspace image to Docker Hub or another registry.
- Native SSH access is exposed by host port mapping in phase 1.
- Traefik/sslip.io hostname generation is optional metadata for future ingress integration; raw SSH over shared port 443 remains a phase-2 problem.
- The workspace image is defined in `docker\workspace\Dockerfile` and configures SSH, rootless Docker, tmux, repo clone, dotfiles, and Playwright tooling.
