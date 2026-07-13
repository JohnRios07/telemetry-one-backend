# Testing Deployment

## Architecture

```
                     GitHub Actions                        VPS (Ubuntu 24.04 ARM64)
┌─────────────────────────────────────┐      ┌────────────────────────────────────┐
│  push → develop                     │      │  /opt/telemetry-one/backend/       │
│  ┌─────────────────┐                │ SSH  │  ├── compose.testing.yaml          │
│  │ Build & Push     │───────────────│─────▶│  ├── .env                          │
│  │ to GHCR          │               │      │  └── (no git clone)                │
│  └─────────────────┘                │      │                                    │
│  ┌─────────────────┐                │      │  docker compose pull + up -d       │
│  │ Deploy over SSH  │───────────────│─────▶│  health check :8081/health         │
│  └─────────────────┘                │      │                                    │
└─────────────────────────────────────┘      └────────────────────────────────────┘
```

Key decisions:

- **No repo clone on VPS.** The compose file is written to the VPS via SSH heredoc; the image is pulled from GHCR. The VPS never needs a GitHub token or deploy key.
- **GHCR for images.** The repo is public, so pulling the image does not require authentication unless the package is later made private. If you make it private, run `docker login ghcr.io` on the VPS with a personal access token.
- **linux/arm64 only.** The VPS is ARM64; the CI build targets `linux/arm64` exclusively to keep builds fast. If you later add amd64 runners, add the platform to `docker/build-push-action`.
- **Free-tier friendly.** GitHub Actions hosted runners, GHCR free tier (1 GB free, public images free), no external CI service.

## Required Secrets

Set these in the GitHub repository → Settings → Secrets and variables → Actions:

| Secret | Description |
|--------|-------------|
| `VPS_HOST` | VPS IP or hostname |
| `VPS_USER` | SSH user (e.g., `ubuntu`, `deploy`) |
| `VPS_PORT` | SSH port (optional, defaults to 22) |
| `VPS_SSH_PRIVATE_KEY` | SSH private key for the deploy user |

The `GITHUB_TOKEN` secret is automatically provided by GitHub Actions for `packages: write`.

## VPS Prerequisites

- Ubuntu 24.04 ARM64
- Docker 28.1.1+ (verify with `docker --version`)
- Docker Compose v2.35.1+ (verify with `docker compose version`)
- `curl` (used by health check)

One-time setup:

```sh
# Install Docker if not present
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker "$USER"
# log out and back in

# Verify
docker --version
docker compose version
```

The deploy user (`ubuntu`) **must have passwordless sudo** (`sudo ALL=(ALL) NOPASSWD:ALL` in `/etc/sudoers.d/90-cloud-init-users`). The first deploy uses it to create `/opt/telemetry-one/backend` and then `chown`s it to the deploy user so subsequent runs do not need sudo.

## First Deploy

1. Push to `develop` (or trigger manually via GitHub Actions → Deploy Testing → Run workflow).
2. The CI pipeline will:
   - Build the Docker image for `linux/arm64`.
   - Push to `ghcr.io/johnrios07/telemetry-one-backend:develop`.
   - SSH into the VPS and create `/opt/telemetry-one/backend/`.
   - Write `compose.testing.yaml` and a default `.env` if missing.
   - Run `docker compose pull` and `docker compose up -d`.
   - Wait for the health endpoint to respond.
3. Verify: `curl http://<vps-ip>:8081/health`

## Rollback

The `deploy-testing.yml` workflow always pulls `:develop`. To roll back:

```sh
ssh user@host
cd /opt/telemetry-one/backend
# Tag the previous working image locally
docker tag ghcr.io/johnrios07/telemetry-one-backend:develop-sha-abc1234 ghcr.io/johnrios07/telemetry-one-backend:develop
docker compose up -d
```

Or push a revert commit to `develop` and let CI redeploy.

## Logs

```sh
ssh user@host
cd /opt/telemetry-one/backend
docker compose logs -f
```

## Health Check

```sh
curl -fsS http://127.0.0.1:8081/health
# → {"status":"ok","env":"testing","time":"2025-01-01T00:00:00Z"}
```

The Docker Compose healthcheck runs every 30s against the container's internal `localhost:8080/health`. Docker considers the container healthy after 3 consecutive successful checks.

## Provider Fake / OpenRouter

The default `.env` sets `TELEMETRY_ONE_AI_PROVIDER=fake`, which returns mock AI responses without any API key. This is safe for the testing environment.

To enable real AI analysis, uncomment the `TELEMETRY_ONE_OPENROUTER_*` vars in `.env` and set a valid OpenRouter API key. The deployment workflow does NOT touch `.env` after creation — key management is your responsibility.

## Why No Repo Clone?

Cloning the full repo on the VPS would:
- Require a GitHub deploy key or PAT stored on the VPS.
- Pull source, docs, testdata, etc. that the runtime does not need.
- Make rollback harder (no image tags to pin).

With GHCR + SSH, the VPS only receives what Docker needs: the image layers.

## Flutter Secrets

No OpenRouter keys or other secrets are exposed to the Flutter client. AI analysis is handled entirely server-side.

## Traefik / Easypanel

The testing deploy uses host port `8081` directly. When you add Traefik or Easypanel later, you can:

- Stop the direct port mapping (`ports` → remove or comment out).
- Add `networks:` to attach the container to the Traefik network.
- Add Traefik labels for TLS and routing.
- Remove the healthcheck override (Traefik has its own).

## ARM64 Note

The VPS runs Ubuntu 24.04 ARM64. All Docker images are built for `linux/arm64`. If you test locally on an amd64 machine, build with `docker build --platform linux/amd64 .` or remove the platform constraint from the Dockerfile. The CI workflow only pushes `linux/arm64` to keep builds fast.
