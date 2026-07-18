# Telemetry One Backend

Go backend for Telemetry One V2.

## Local Development

Run the API locally:

```sh
go run ./cmd/api
```

Run local Postgres for durable persistence testing:

```sh
docker compose -f compose.local.yaml up -d postgres
TELEMETRY_ONE_DATABASE_URL='postgres://telemetry:telemetry@localhost:5432/telemetry_one?sslmode=disable' go run ./cmd/api
docker compose -f compose.local.yaml down
```

Leave `TELEMETRY_ONE_DATABASE_URL` unset to keep the default in-memory mode.

Run tests:

```sh
go test ./...
```

## Configuration

Environment variables:

| Name | Default | Description |
| --- | --- | --- |
| `TELEMETRY_ONE_ADDR` | `:8080` | HTTP listen address. |
| `TELEMETRY_ONE_ENV` | `development` | Runtime environment label. |
| `TELEMETRY_ONE_LOG_LEVEL` | `info` | JSON log level: `debug`, `info`, `warn`, or `error`. |
| `TELEMETRY_ONE_READ_HEADER_TIMEOUT` | `5s` | HTTP read header timeout as a Go duration. |
| `TELEMETRY_ONE_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown timeout as a Go duration. |
| `TELEMETRY_ONE_RETAINED_FRAMES_PER_SESSION` | `12000` | Maximum accepted normalized frames retained in memory per session. |
| `TELEMETRY_ONE_DATABASE_URL` | `` | Optional Postgres URL. When set, sessions use durable Postgres persistence and embedded migrations run at startup. When empty, sessions stay in-memory. |
| `TELEMETRY_ONE_ADMIN_TOKEN` | `` | Optional admin auth token. When set, `GET /api/v1/admin/ingest-stats` requires `Authorization: Bearer <token>`. When empty, the endpoint is open. |
| `TELEMETRY_ONE_SKIP_INVALID_APPROVED_GEOMETRY` | `false` | When `true`, invalid runtime-approved geometry entries are skipped during startup instead of failing closed. |

## Service Conventions

- Logging uses structured JSON via `log/slog`.
- API errors use stable machine-readable codes and the JSON envelope `{ "error": { "code": "...", "message": "..." } }`.

## Persistence

PostgreSQL is optional at runtime through `TELEMETRY_ONE_DATABASE_URL`. If configured, the API opens a pgx pool, runs embedded migrations, and stores session lifecycle data in Postgres. If empty, local development and tests keep the in-memory session repository.

For local Postgres, use `compose.local.yaml` and the development URL `postgres://telemetry:telemetry@localhost:5432/telemetry_one?sslmode=disable`.

## Synthetic Simulator

Phase 1.5 adds deterministic synthetic telemetry fixtures for development without a PS5. The data is fictional, not official Gran Turismo 7 metadata, and must not be used as a source of track or corner names.

Generate a JSON batch payload:

```sh
go run ./cmd/simfixture -hz 20 -duration 9 -session synthetic-session-001
```

See `docs/simulator.md` for details and tradeoffs.

## Telemetry Normalization

Phase 2.1 defines the canonical frame units and validation rules in `docs/telemetry-normalization.md`. Missing yaw, wheel speed, and historical lap-time fields stay explicit unknowns; the backend does not invent source telemetry.

Phase 2.4 keeps accepted normalized frames in a bounded in-memory per-session buffer. This feeds the upcoming track, distance, and event engines while avoiding PostgreSQL raw-frame persistence in the MVP. The tradeoff is intentional: retained frames are fast and simple but ephemeral, process-local, and evict oldest samples past the configured bound.

## Ingest Performance

Phase 2.5 documents expected 20/30/60 Hz frame volumes, batch-size tradeoffs, retention-window implications, and focused Go benchmarks in `docs/ingest-performance.md`.

## Track Catalog Metadata

Phase 3.1 defines the versioned track/layout/sector/corner catalog format in `docs/track-catalog.md`, with a JSON Schema in `docs/track-catalog.schema.json`. The included `testdata/catalogs/synthetic_dev_catalog.json` fixture is synthetic/dev-only and is not official Gran Turismo 7 metadata.

## Testing Deployment

See [docs/testing-deployment.md](docs/testing-deployment.md) for the CI/CD architecture using GitHub Actions, GHCR, and SSH deployment to an ARM64 VPS.

Testing deployment requires a `VPS_POSTGRES_PASSWORD` GitHub Actions secret. Because that value is embedded directly in the Postgres URL, generate it with URL-safe characters only, for example:

```sh
python3 -c "import secrets; print(secrets.token_urlsafe(32))"
```
