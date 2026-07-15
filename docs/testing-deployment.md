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
│  ┌─────────────────┐                │                                                  │  sudo docker compose pull + up -d       │
│  │ Deploy over SSH  │───────────────│─────▶│  health check :8081/health         │
│  └─────────────────┘                │      │                                    │
└─────────────────────────────────────┘      └────────────────────────────────────┘
```

Key decisions:

- **No repo clone on VPS.** The compose file is written to the VPS via SSH heredoc; the image is pulled from GHCR. The VPS never needs a GitHub token or deploy key.
- **Postgres in testing.** The testing compose stack runs a private `postgres:16-alpine` service with a persistent `pgdata` volume. The backend connects through Docker DNS (`postgres:5432`) when `TELEMETRY_ONE_DATABASE_URL` is set, and embedded migrations run at backend startup.
- **GHCR for images.** The repo is public, so pulling the image does not require authentication unless the package is later made private. If you make it private, run `docker login ghcr.io` on the VPS with a personal access token.
- **linux/arm64 only.** The VPS is ARM64; the CI build targets `linux/arm64` exclusively to keep builds fast. If you later add amd64 runners, add the platform to `docker/build-push-action`.
- **Free-tier friendly.** GitHub Actions hosted runners, GHCR free tier (1 GB free, public images free), no external CI service.

## Required Secrets

Set these in the GitHub repository → Settings → Secrets and variables → Actions → Secrets:

| Secret | Description |
|--------|-------------|
| `VPS_HOST` | VPS IP or hostname |
| `VPS_USER` | SSH user (e.g., `ubuntu`, `deploy`) |
| `VPS_PORT` | SSH port (optional, defaults to 22) |
| `VPS_SSH_PRIVATE_KEY` | SSH private key for the deploy user |
| `VPS_POSTGRES_PASSWORD` | Testing Postgres password used for `POSTGRES_PASSWORD` and embedded directly in `TELEMETRY_ONE_DATABASE_URL`; must contain only URL-safe unreserved characters (`A-Z`, `a-z`, `0-9`, `_`, `.`, `~`, `-`). Generate with `python3 -c "import secrets; print(secrets.token_urlsafe(32))"`. |

The `GITHUB_TOKEN` secret is automatically provided by GitHub Actions for `packages: write`. If `VPS_POSTGRES_PASSWORD` is absent or contains URL-unsafe characters, deployment fails before touching Docker so testing never boots with a weak default password or malformed Postgres URL.

## Optional Secrets (AI Provider)

Set these in GitHub Secrets only when you want to enable real OpenRouter AI on the testing VPS:

| Secret | Description |
|--------|-------------|
| `TELEMETRY_ONE_OPENROUTER_API_KEY` | OpenRouter API key. Required when `TELEMETRY_ONE_AI_PROVIDER` variable is set to `openrouter`. Ignored when provider is `fake`. Never logged or exposed to clients. |

> **Security**: The API key is passed via `envs:` to the SSH session, written to `.env` (permissions `600`), and never appears in logs. No `set -x` shell tracing is used. The key stays server-side on the VPS — Flutter never receives it.

## Optional Variables (AI Provider)

Set these in GitHub Variables (Settings → Secrets and variables → Actions → Variables) to opt into real AI provider behavior:

| Variable | Default | Description |
|----------|---------|-------------|
| `TELEMETRY_ONE_AI_PROVIDER` | `fake` | Set to `openrouter` to enable real OpenRouter AI. Unset/empty → workflow enforces `fake` on every deploy. |
| `TELEMETRY_ONE_AI_MODEL` | `gpt-4o-mini` | Override the model identifier (e.g., `gpt-4o`, `claude-3-haiku`). Must be supported by OpenRouter. When unset, existing `.env` value is preserved (initial default is `gpt-4o-mini`). |

> **Provider opt-in is explicit**: The workflow never switches from `fake` to `openrouter` automatically. You MUST set the `TELEMETRY_ONE_AI_PROVIDER` variable to `openrouter` AND provide the `TELEMETRY_ONE_OPENROUTER_API_KEY` secret. Having a key present without setting the provider keeps the safe `fake` default.

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

The deploy user (`ubuntu`) **must have passwordless sudo** (`sudo ALL=(ALL) NOPASSWD:ALL` in `/etc/sudoers.d/90-cloud-init-users`) for both the initial `/opt` directory creation and all Docker commands (`sudo docker login`, `sudo docker compose pull`, `sudo docker compose up -d`).

> If the user is later added to the `docker` group (via `usermod -aG docker "$USER"` followed by a new login), `sudo` can be dropped from the Docker commands — but the initial deploy will not benefit from this since the SSH session used by GitHub Actions does not re-login.

## First Deploy

1. Push to `develop` (or trigger manually via GitHub Actions → Deploy Testing → Run workflow).
2. The CI pipeline will:
   - Build the Docker image for `linux/arm64`.
   - Push to `ghcr.io/johnrios07/telemetry-one-backend:develop`.
   - SSH into the VPS and create `/opt/telemetry-one/backend/`.
   - Write `compose.testing.yaml`.
   - Create `.env` if missing, or append missing Postgres keys to an existing `.env` without overwriting existing values. This append-only migration is intentional: it does not prune obsolete keys from the VPS `.env`.
   - Set `.env` permissions to `600`.
   - Run `sudo docker login`, `sudo docker compose pull`, and `sudo docker compose up -d`.
   - Wait up to 120 seconds for the health endpoint to respond, allowing time for first Postgres initialization and migrations.
3. Verify: `curl http://<vps-ip>:8081/health`

The workflow preserves the no-repo-clone model: only `compose.testing.yaml` and `.env` live under `/opt/telemetry-one/backend/` on the VPS.

## Local Postgres Development

Use the shareable local Compose file when you want to exercise the real Postgres path:

```sh
docker compose -f compose.local.yaml up -d postgres
TELEMETRY_ONE_DATABASE_URL='postgres://telemetry:telemetry@localhost:5432/telemetry_one?sslmode=disable' go run ./cmd/api
docker compose -f compose.local.yaml down
```

Use `docker compose -f compose.local.yaml down -v` only when you intentionally want to delete the local Postgres volume.

When `TELEMETRY_ONE_DATABASE_URL` is not set, the backend keeps using the in-memory repositories for local development and tests.

## Testing Postgres Operations

The VPS testing database runs inside Docker only; port `5432` is not exposed on the host. Docker Compose automatically creates the `pgdata` volume on first deploy.

Create a backup from the VPS:

```sh
ssh user@host
cd /opt/telemetry-one/backend
sudo docker compose -f compose.testing.yaml exec -T postgres pg_dump -U telemetry -d telemetry_one > telemetry-one-testing-$(date +%Y%m%d%H%M%S).sql
```

Inspect services and health:

```sh
sudo docker compose -f compose.testing.yaml ps
sudo docker compose -f compose.testing.yaml logs -f postgres telemetry-one-backend
```

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

### Rollback AI Provider

To disable OpenRouter and return to `fake`:

1. Remove or unset the `TELEMETRY_ONE_AI_PROVIDER` GitHub Variable.
2. Either:
   - **Wait for next deploy**: The workflow will explicitly set `TELEMETRY_ONE_AI_PROVIDER=fake` (via the `upsert_env` else branch). The stale `TELEMETRY_ONE_OPENROUTER_API_KEY` (if any) is left in `.env` but ignored — the `fake` provider never calls OpenRouter.
   - **SSH approach** (immediate):
     ```sh
     ssh user@host
     cd /opt/telemetry-one/backend
     sed -i '/^TELEMETRY_ONE_AI_PROVIDER=/d' .env
     sed -i '/^TELEMETRY_ONE_OPENROUTER_API_KEY=/d' .env
     sudo docker compose -f compose.testing.yaml up -d
     ```
3. Verify: `curl http://<vps-ip>:8081/health`

The container picks up the new `.env` on restart — no rebuild needed.

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

## Provider Fake / OpenRouter (Deploy-Time)

The default `.env` always starts with `TELEMETRY_ONE_AI_PROVIDER=fake`, which returns mock AI responses without any API key. This is safe for the testing environment by default.

### How Opt-In Works

1. **Set the Variable**: Create a GitHub Variable `TELEMETRY_ONE_AI_PROVIDER` with value `openrouter`.
2. **Set the Secret**: Create a GitHub Secret `TELEMETRY_ONE_OPENROUTER_API_KEY` with your OpenRouter key.
3. **Deploy**: Push to `develop` (or trigger workflow manually).

On every deploy, the workflow checks if these values are set:

- GitHub Variable set → `upsert_env` writes/updates the `.env` entry.
- GitHub Variable unset/removed → `upsert_env` writes `TELEMETRY_ONE_AI_PROVIDER=fake`, restoring safe default.
- GitHub Secret present → `upsert_env` writes/updates `TELEMETRY_ONE_OPENROUTER_API_KEY`.
- GitHub Secret absent → key line stays in `.env` if present (harmless when provider is `fake`).
- New key added → `.env` gains the new entry.
- Key updated in GitHub → `.env` gets the new value on next deploy.
- To fully remove both: SSH into the VPS and delete the lines from `/opt/telemetry-one/backend/.env`, then restart the container.

### Default: `fake` (Safe)

Every deploy enforces `TELEMETRY_ONE_AI_PROVIDER=fake` unless the GitHub Variable explicitly overrides it:

```
TELEMETRY_ONE_AI_PROVIDER=fake         ← enforced by workflow else-branch
TELEMETRY_ONE_OPENROUTER_API_KEY=(not set)
TELEMETRY_ONE_AI_MODEL=gpt-4o-mini     ← initial .env default
```

### Opt-In: `openrouter`

Requires both (otherwise stays `fake`):

| What | Where | Value |
|------|-------|-------|
| Variable | `TELEMETRY_ONE_AI_PROVIDER` | `openrouter` |
| Secret | `TELEMETRY_ONE_OPENROUTER_API_KEY` | `sk-or-v1-...` |
| Variable (optional) | `TELEMETRY_ONE_AI_MODEL` | Any OpenRouter model |

### Why Not Auto-Switch on Key Presence?

If a key is set but the provider variable is not, the workflow actively enforces `TELEMETRY_ONE_AI_PROVIDER=fake` via its else branch. This prevents accidental OpenRouter usage (and cost) if someone adds a key for future use without intending to switch. The switch is intentional and explicit.

Conversely, if the provider variable is later removed, the workflow re-enforces `fake` automatically — no manual `.env` editing required unless you also want to clean up the stale API key line.

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
