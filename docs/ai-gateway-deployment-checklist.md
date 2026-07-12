# AI Gateway Deployment Checklist (Phase 7.10)

## Environment Variables

All configuration is via environment variables loaded in `internal/config/config.go`. No `.env` file is shipped or required — the server reads from the process environment.

### Server

| Variable | Default | Required | Notes |
|----------|---------|----------|-------|
| `TELEMETRY_ONE_ADDR` | `:8080` | No | Listen address for the HTTP server |
| `TELEMETRY_ONE_ENV` | `development` | No | `development` or `production`; affects health endpoint and may influence log format |
| `TELEMETRY_ONE_LOG_LEVEL` | `info` | No | `debug`, `info`, `warn`, `error` |
| `TELEMETRY_ONE_READ_HEADER_TIMEOUT` | `5s` | No | Go `http.Server.ReadHeaderTimeout` duration |
| `TELEMETRY_ONE_SHUTDOWN_TIMEOUT` | `10s` | No | Graceful shutdown drain timeout |
| `TELEMETRY_ONE_RETAINED_FRAMES_PER_SESSION` | `12000` | No | Max frames kept in memory per session |

### Provider Selection

| Variable | Default | Required | Notes |
|----------|---------|----------|-------|
| `TELEMETRY_ONE_AI_PROVIDER` | `fake` | No | `"fake"` or `"openrouter"`. Fake returns deterministic responses with no API key. OpenRouter requires `TELEMETRY_ONE_OPENROUTER_API_KEY`. |

### OpenRouter (only when provider = `"openrouter"`)

| Variable | Default | Required | Notes |
|----------|---------|----------|-------|
| `TELEMETRY_ONE_OPENROUTER_API_KEY` | — | Yes* | Backend-only. NEVER in Flutter, client code, or version control. *Required when provider is `"openrouter"`; ignored when provider is `"fake"`. |
| `TELEMETRY_ONE_OPENROUTER_BASE_URL` | `https://openrouter.ai/api/v1` | No | Override for OpenRouter API base URL. Useful for testing with a mock server (e.g., `http://localhost:8099`). |
| `TELEMETRY_ONE_OPENROUTER_HTTP_REFERER` | — | No | Optional `HTTP-Referer` header for OpenRouter attribution |
| `TELEMETRY_ONE_OPENROUTER_TITLE` | — | No | Optional `X-OpenRouter-Title` header for OpenRouter attribution |

### Model Configuration

| Variable | Default | Required | Notes |
|----------|---------|----------|-------|
| `TELEMETRY_ONE_AI_MODEL` | `gpt-4o-mini` | No | Model identifier passed to the provider. Must be supported by OpenRouter if using openrouter provider. |

### Budget / Token Limits

| Variable | Default | Required | Notes |
|----------|---------|----------|-------|
| `TELEMETRY_ONE_AI_MAX_PROMPT_CHARS` | `40000` | No | Max characters in the assembled prompt (system + user). Exceeding returns `ErrBudgetExceeded` before any provider call — no cost incurred. |
| `TELEMETRY_ONE_AI_MAX_COMPLETION_TOKENS` | `2000` | No | Max tokens requested from the provider for the completion |
| `TELEMETRY_ONE_AI_MAX_EVENTS_PER_REQUEST` | `50` | No | Max events the gateway accepts per analyze request |

### Retry Policy

| Variable | Default | Required | Notes |
|----------|---------|----------|-------|
| `TELEMETRY_ONE_AI_RETRY_MAX_ATTEMPTS` | `3` | No | Total attempts including the first. 1 = no retries. |
| `TELEMETRY_ONE_AI_RETRY_BASE_BACKOFF_MS` | `1000` | No | Exponential backoff base in milliseconds |
| `TELEMETRY_ONE_AI_RETRY_MAX_BACKOFF_MS` | `10000` | No | Cap for exponential backoff in milliseconds |

### Rate Limiting

| Variable | Default | Required | Notes |
|----------|---------|----------|-------|
| `TELEMETRY_ONE_AI_RATE_PER_SECOND` | `10.0` | No | Token bucket refill rate per session |
| `TELEMETRY_ONE_AI_RATE_BURST` | `5` | No | Token bucket burst size per session |

### Cost Estimation

| Variable | Default | Required | Notes |
|----------|---------|----------|-------|
| `TELEMETRY_ONE_AI_COST_PER_PROMPT_TOKEN` | `0.00015` | No | Cost per 1K prompt tokens (used for usage accounting only — NOT enforced) |
| `TELEMETRY_ONE_AI_COST_PER_COMPLETION_TOKEN` | `0.00060` | No | Cost per 1K completion tokens (used for usage accounting only — NOT enforced) |

---

## Provider Selection Behavior

- **`fake` (default)**: `FakeProvider` in `internal/ai/compose.go` returns deterministic responses. No API key required. No network calls. Use for local development, CI, and integration tests.
- **`openrouter`**: `OpenRouterAdapter` in `internal/ai/openrouter.go` makes real HTTP calls to OpenRouter. Requires `TELEMETRY_ONE_OPENROUTER_API_KEY`. Subject to retry, rate limiting, and fallback.
- Switching: set `TELEMETRY_ONE_AI_PROVIDER=openrouter` in the deployment environment. The switch is runtime — no recompile needed.

> **No early validation of the API key.** An empty or invalid key results in HTTP 401 from OpenRouter, which is mapped to `ErrProviderRejected` (non-retryable), and the `FallbackService` returns a deterministic degraded response.

---

## Credential Security

- API key (`TELEMETRY_ONE_OPENROUTER_API_KEY`) is **backend-owned and server-side only**.
- API key is **never in Flutter** or any client bundle.
- API key is **never logged** — it exists only in the `Config` struct and is passed directly to HTTP headers.
- No `.env` files are committed. Use your deployment platform's secret management (environment variables, vault, etc.).
- On `development` with `fake` provider, no key is needed at all.

---

## No Raw Telemetry / No Flutter Secrets

This is enforced at multiple architectural layers — not just convention:

| Layer | Enforcement |
|-------|-------------|
| `ConsumerInput.Validate()` | Rejects raw telemetry metric names via strict JSON decoding with `DisallowUnknownFields()` |
| `ContextBuilder` | Only includes catalog names, event types, severities, lap/corner metadata — never frames, positions, speeds |
| `GatewayRequest` | Wraps validated `ConsumerInput`; rejects at the boundary |
| `AuditedService` | Records `RedactionVerified: true` after validation |
| Source of truth | Track/layout/corner names come from catalog metadata only, never from GT7 UDP |

> **Rule**: If raw telemetry (speed, throttle, brake, position, yaw, wheel speeds, fuel) appears in any AI-bound payload, it is a bug. The validation layer should catch it; if it doesn't, the deployment pipeline must reject it.

---

## Audit / Fallback Behavior

### Audit

- `AuditedService` wraps the pipeline and records every analyze request.
- Audit records are written to `MemoryAuditStore` (in-memory, per-process). Production deployments should swap for a persistent store implementing the same `AuditLogger` interface.
- Format: `AuditRecord` with trace ID, session ID, mode, event IDs, provider model, token usage, duration, status.
- Constraints: only counts are stored (e.g., "3 constraints"), never full constraint text.
- Response content: only first 500 characters of the summary are stored by default.
- Audit **never** contains: raw telemetry, API keys, full constraint text, or full response content.
- Audit failures are best-effort — they never fail the original request.

### Fallback FallbackStatus Values

| Status | Trigger | Behavior |
|--------|---------|----------|
| `success` | Provider returned valid response | Normal AI analysis with explanations and recommendations |
| `provider_error` | Provider unavailable, timeout, retries exhausted | Deterministic fallback: "AI analysis unavailable" with top 3 high-severity events and their metric evidence |
| `budget_limited` | Token/event budget exceeded before provider call | Same fallback as provider_error |
| `rate_limited` | Rate limiter blocked the request | Same fallback as provider_error |
| `invalid_response` | Provider returned empty summary or missing referenced events | Same fallback as provider_error |

Fallback content is safe, deterministic, and sourced exclusively from events and catalog metadata — no fabricated AI text, no raw telemetry.

---

## Testing Without Live Network

All AI gateway tests use mocked HTTP servers — no live OpenRouter calls, no real API key required.

### Test Categories

| Test File | What It Covers |
|-----------|----------------|
| `internal/ai/service_test.go` | Service layer: valid/invalid requests, budget exceed, retry exhaustion, non-retryable errors, model override, coach mode |
| `internal/ai/controls_test.go` | Token budget, retry policy, rate limiter, cost estimation, usage accounting |
| `internal/ai/fallback_test.go` | Fallback response for all error types |
| `internal/ai/audit_test.go` | Audit record structure, redaction, best-effort logging |
| `internal/ai/compose_test.go` | Pipeline composition: fake provider, openrouter adapter selection, fallback cascade |
| `internal/ai/openrouter_test.go` | HTTP requests via `httptest.NewServer`: success, 500, 429, 401, invalid response, network errors |
| `internal/api/ai_e2e_test.go` | End-to-end from HTTP handler through full pipeline to mock OpenRouter server |

### Run Tests

```bash
# All AI gateway tests (no network, no API key)
go test ./internal/ai/... -v -count=1

# All API tests including E2E
go test ./internal/api/... -v -count=1

# Full backend test suite
go test ./... -v -count=1

# Exclude tests matching a pattern (unlikely needed — all tests are offline)
go test ./... -run "^Test[^L]" -count=1
```

> **Zero tests make live network calls.** If a test connects to an external host, it is a bug. The `httptest.NewServer` pattern is used throughout to simulate OpenRouter responses.

---

## Manual Smoke Test (Development)

For quick manual verification without exposing credentials:

```bash
# 1. Start the server with fake provider (default)
TELEMETRY_ONE_AI_PROVIDER=fake go run ./cmd/server

# 2. Send a test analyze request
curl -s -X POST http://localhost:8080/api/v1/sessions/test-123/analyze \
  -H 'Content-Type: application/json' \
  -d '{
    "input": {
      "contractVersion": "telemetry-one.v1",
      "session": {
        "sessionId": "test-123",
        "trackId": "tsukuba",
        "trackName": "Tsukuba Circuit",
        "layoutId": "full",
        "layoutName": "Full Course",
        "lapCount": 1,
        "totalTimeMs": 60000
      },
      "events": [
        {
          "eventId": "evt-001",
          "sessionId": "test-123",
          "type": "braking",
          "severity": "high",
          "lapNumber": 1,
          "cornerId": "turn1",
          "cornerName": "Turn 1",
          "metricEvidence": {
            "brakePressure": 85.0,
            "speedDropMs": 12.5
          },
          "timestampUnixMs": 1000,
          "description": "Late braking into Turn 1"
        }
      ],
      "safety": {
        "minSpeedMps": 10.0,
        "maxSpeedMps": 80.0,
        "maxLateralGs": 1.2
      }
    },
    "mode": "engineer"
  }' | jq .

# 3. Expected: status "success" with deterministic AI analysis from FakeProvider
```

For OpenRouter smoke test (requires key, but safe — no real data is sent if done with known test events):

```bash
TELEMETRY_ONE_AI_PROVIDER=openrouter \
TELEMETRY_ONE_OPENROUTER_API_KEY=sk-or-v1-... \
go run ./cmd/server
```

> Note: The smoke test response will come from the actual OpenRouter model. Use known test data only — never real user sessions during development.

---

## Quick Reference: No-Go Items

| ❌ Do not | ✅ Instead |
|-----------|------------|
| Store API key in Flutter or client bundle | Keep key in backend environment variables |
| Commit `.env` with real secrets to git | Use deployment platform secret management |
| Send raw telemetry frames to AI | Send structured events and derived metrics only |
| Invent track/corner names from UDP | Use catalog metadata; null for unknown |
| Test with live OpenRouter in CI | Use `FakeProvider` or `httptest.NewServer` |
| Expose API key in error messages or logs | Log only operational info (model, status, duration) |
| Send full constraint text to AI | Send only count of constraints |
| Store full response content in audit | Store first 500 chars of summary only |
| Make deployment depend on OpenRouter being up | Fallback is designed for graceful degradation |
