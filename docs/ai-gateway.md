# AI Gateway

Phase 7.1 defines the provider-neutral AI gateway design. It sits between the backend core (deterministic event engine, catalog, session context) and external AI providers. The gateway ensures AI receives only structured Engineer events and derived metrics — NEVER raw telemetry frames.

## Architecture

```
┌─────────────────────────────┐
│      Application Layer      │
│  (API handlers, services)   │
└──────────┬──────────────────┘
           │ GatewayRequest (ConsumerInput + Mode)
           ▼
┌─────────────────────────────┐
│       AIService (interface) │  ← Provider-neutral core
│  - Analyze(ctx, req) -> resp│
└──────────┬──────────────────┘
           │ ProviderRequest
           ▼
┌─────────────────────────────┐
│    ProviderAdapter (iface)  │  ← Pluggable: OpenRouter, OpenAI, etc.
│  - Analyze(ctx, req) -> resp│
└──────────┬──────────────────┘
           │ HTTP/SSE to external API
           ▼
┌─────────────────────────────┐
│     AI Provider (external)  │
│  OpenRouter / OpenAI / etc. │
└─────────────────────────────┘
```

## Key Design Decisions

### Provider-Neutral Core
- `AIService` interface uses no provider-specific types. It accepts `GatewayRequest` and returns `GatewayResponse`.
- `ProviderAdapter` interface is the only provider-aware boundary. OpenRouter adapter is Phase 7.2+.
- The application layer never imports OpenRouter or any provider SDK directly.

### Gateway Request
- Wraps `ConsumerInput` (Phase 5.5 contract) and a `Mode` field (`engineer` or `coach`).
- `Validate()` calls `ConsumerInput.Validate()` which rejects raw telemetry via strict JSON decoding.
- Raw telemetry metric names (`speedMps`, `throttle`, `brake`, etc.) are rejected at the event metric level.

### Gateway Response
- Structured, not free-form: `Summary`, `EventExplanations[]`, `Recommendations[]`, `ProviderInfo`.
- Each `EventExplanation` links back to the source `EventID` for traceability.
- `ReferencedEvents` contains all event IDs the gateway processed.

### Context Builder
- `ContextBuilder` converts `ConsumerInput` into a `ContextSummary` struct.
- `BuildPromptSummary()` creates a formatted text summary suitable for embedding into AI prompts.
- The builder never includes raw telemetry fields. It uses only catalog names, event types, severities, confidence, lap numbers, corner names, and derived metric evidence.
- Unknown catalog refs (nil track/layout/corner) produce empty strings, never invented names.

### Backend-Owned Credentials
- Provider API keys live in backend configuration/environment, never in Flutter.
- No app-owned API keys are shipped in the mobile app.
- Future user-owned OAuth/PKCE flows are possible but out of scope for MVP.

## Interfaces

### AIService (internal/ai/gateway.go)

```go
type AIService interface {
    Analyze(ctx context.Context, req GatewayRequest) (GatewayResponse, error)
}
```

### ProviderAdapter (internal/ai/gateway.go)

```go
type ProviderAdapter interface {
    Analyze(ctx context.Context, req ProviderRequest) (ProviderResponse, error)
}
```

## Types

See `internal/ai/gateway.go` for:

| Type | Purpose |
| --- | --- |
| `GatewayRequest` | Input wrapping ConsumerInput + mode. Validated no raw telemetry. |
| `GatewayResponse` | Structured output with explanations, recommendations, traceability. |
| `EventExplanation` | Per-event AI analysis with relevance score. |
| `ProviderRequest` | Provider-agnostic request (model, messages, temperature, maxTokens). |
| `ProviderResponse` | Provider-agnostic response (content, model, finish reason, usage). |
| `UsageInfo` | Token usage from provider. |
| `ContextBuilder` | Builds structured ContextSummary from ConsumerInput. |
| `ContextSummary` | Structured summary with Session, Events[], Safety sections. |
| `EventSummary` | Per-event summary for prompt context. |

## Validation Flow

1. Gateway input is JSON-decoded with `DisallowUnknownFields()` — raw telemetry fields like `frames`, `positionX`, `speedMps`, etc. are rejected.
2. `Validate()` checks contract version, session context, events, safety metadata.
3. Event metric validation rejects raw telemetry metric names.
4. Unknown catalog refs are allowed (nil is valid) — no names are invented.

## Traceability

- `GatewayResponse.ReferencedEvents` lists all processed event IDs.
- `EventExplanation.EventID` links each explanation to its source event.
- `ProviderResultInfo` includes the model and finish reason for observability.

## Audit Logging (Phase 7.4)

Phase 7.4 adds response-to-event source traceability and audit logging via the `AuditedService` wrapper.

### Architecture

```
GatewayRequest → AuditedService.Analyze()
                    │
                    ├── traceID = idProvider.NewID()
                    ├── inner.Analyze(ctx, req)
                    ├── build AuditRecord (event IDs, model, usage, timing, status)
                    └── logger.Record(ctx, record)  // best-effort, never fails request
```

### AuditRecord

| Field | Description |
| --- | --- |
| `TraceID` | Unique request identifier (UUID or injectable for tests) |
| `SessionID` | Session from GatewayRequest |
| `Mode` | "engineer" or "coach" |
| `SourceEventIDs` | All event IDs from the request |
| `ProviderModel` | Model used for the response |
| `PromptTemplateVersion` | `telemetry-one.prompt.v1` (compile-time constant) |
| `ConstraintSummary` | Count of constraints (never full text) |
| `ResponseSummary` | First 500 chars of the response summary |
| `ResponseSummaryLimited` | True when truncation occurred |
| `PromptTokenCount` / `CompletionTokenCount` / `TotalTokenCount` | Token usage |
| `DurationMs` | Request duration in milliseconds |
| `Status` | "success" or "error" |
| `ErrorMessage` | Error details (empty on success) |
| `RedactionVerified` | Always true — ConsumerInput validation guarantees no raw telemetry |
| `CreatedAt` | Timestamp of the audit record |

### Key Design Decisions

- **Wrapping, not coupling**: `AuditedService` wraps `AIService` via the decorator pattern. The core `AIService` interface is unchanged.
- **Best-effort audit logging**: Audit failures never fail the original request.
- **Response summarization by default**: Only the first 500 chars of the response summary are stored. Full content storage requires explicit opt-in (tradeoff: loss of detail vs data minimization).
- **Constraint summary, not full text**: `ConstraintSummary` stores only the count of constraints (e.g., `"3 constraints"`). Full constraint text is excluded to prevent leakage of application-specific rules.
- **No raw telemetry in audit records**: `RedactionVerified: true` relies on `ConsumerInput.Validate()` which rejects raw telemetry fields and metric names before the GatewayRequest reaches the audit layer.
- **Provider credentials never logged**: API keys and provider tokens are outside the data flow — they exist only in server config.
- **Injectability**: `IDProvider` interface allows UUIDs in production and `SequentialProvider` for deterministic test IDs.
- **In-memory storage**: `MemoryAuditStore` implements `AuditLogger` for MVP. Production would swap in a database-backed implementation that satisfies the same interface.

### AuditLogger Interface

```go
type AuditLogger interface {
    Record(ctx context.Context, record AuditRecord) error
}
```

### Types

All types in `internal/ai/audit.go`:

| Type | Purpose |
| --- | --- |
| `AuditRecord` | Full trace record |
| `AuditLogger` | Interface for audit storage |
| `MemoryAuditStore` | Thread-safe in-memory implementation |
| `IDProvider` | Interface for trace ID generation |
| `UUIDProvider` | Production UUID generator |
| `SequentialProvider` | Deterministic provider for tests |
| `AuditedService` | Decorator that wraps AIService with audit logging |

### Audit Boundaries

- Audit **never** includes: raw telemetry frames, API keys, full provider credentials, full constraint text, full response content.
- Audit **includes**: event IDs (linking AI answers to source events), token usage, duration, model, mode, success/error status.
- Response content is **summarized** (truncated to 500 chars) by default.
- To store full response content, replace `AuditedService` or inject a custom logger with explicit opt-in.

## Completed Phases

| Phase | Scope | File |
| --- | --- | --- |
| 7.2 | Prompt templates/strategies for Engineer and Coach modes. | `internal/ai/prompts.go`, `docs/ai-prompts.md` |
| 7.3 | Cost control, rate limiting, retries, observability. | `internal/ai/controls.go`, `internal/ai/controls_test.go`, `docs/ai-gateway-controls.md` |
| 7.4 | Response-to-event source traceability, audit logging. | `internal/ai/audit.go`, `internal/ai/audit_test.go`, `docs/ai-gateway.md` |
| 7.5 | Fallback behavior when provider fails or returns invalid responses. | `internal/ai/fallback.go`, `internal/ai/fallback_test.go`, `docs/ai-gateway.md` |
| 7.6 | Concrete pipeline composing PromptBuilder + Controller + ProviderAdapter, exposed via HTTP. | `internal/ai/service.go`, `internal/ai/service_test.go`, `internal/api/routes.go` (`analyzeHandler`, `routesWithAI`), `internal/api/analyze_test.go` |
| 7.8 | Provider selection wiring — `ComposePipeline()` selects fake vs OpenRouter adapter, builds full pipeline Service → Fallback → Audit, injects into routes. | `internal/ai/compose.go`, `internal/ai/compose_test.go`, `internal/api/routes.go` (`routes()` wired) |

## Phase 7.5 — Fallback (Degraded Behavior)

Phase 7.5 adds deterministic fallback behavior when the AI provider fails, times out, is rate/budget limited, or returns malformed output.

### Architecture

FallbackService wraps AIService via the decorator pattern (same pattern as AuditedService):

```
GatewayRequest → AuditedService.Analyze()
                  │
                  └── FallbackService.Analyze()
                        │
                        ├── inner.Analyze() → success → return response with StatusSuccess
                        │
                        └── inner.Analyze() → error → build fallback response
                              ├── provider_error: provider not available, timeout, retry exhausted, canceled
                              ├── budget_limited: budget exceeded before provider call
                              ├── rate_limited: rate limited before provider call
                              └── invalid_response: provider returned empty summary or no referenced events
```

### Fallback Response Contract

The `GatewayResponse.Status` field indicates the outcome. `GatewayResponse.Error` carries the original error message (redacted for audit — no raw telemetry, no credentials, no prompts).

| Status | Meaning |
|--------|---------|
| `success` | Provider returned a valid response |
| `provider_error` | Provider failed after retries, or context canceled |
| `budget_limited` | Token/event budget exceeded before provider call |
| `rate_limited` | Rate limiter blocked before provider call |
| `invalid_response` | Provider returned empty summary or missing referenced events |

The response types `GatewayResponse` was extended with two new fields:

```go
type GatewayResponse struct {
    // ... existing fields ...
    Status string `json:"status"`          // one of the status constants above
    Error  string `json:"error,omitempty"` // original error detail (for operational logging, not end-user display)
}
```

### Deterministic Fallback Content

When fallback occurs, `FallbackService.buildFallbackSummary()` generates content that:
- States AI analysis is unavailable, with the specific reason
- Includes the session ID, track name, and layout name (from catalog metadata only)
- Lists the top 3 highest-severity events with their IDs, types, severities, lap numbers, and corners
- Includes metric evidence for each displayed event
- Mentions the total event count
- No AI explanations, coaching advice, or recommendations
- No raw telemetry fields

### Audit Integration

Fallback is fully auditable. `AuditedService` reads `resp.Status` from the `GatewayResponse` and records it directly in the `AuditRecord.Status` field. The specific status values (`provider_error`, `budget_limited`, `rate_limited`, `invalid_response`) are all recorded, enabling operational analysis of failure patterns.

### Key Design Decisions

- **Decorator pattern**: FallbackService follows the same AIService wrapping pattern as AuditedService. They are composed: `AuditedService(FallbackService(realService))`.
- **Request validation preserved**: Invalid requests (missing contract version, no events, etc.) still return errors — they are not fallback candidates.
- **Empty summary guard**: If the provider returns a response with an empty Summary or nil ReferencedEvents, that's treated as an invalid response and triggers fallback.
- **No AI explanations in fallback**: Fallback content is deterministic and derived exclusively from the ContextBuilder and event data. No fabricated AI text.
- **Audit captures the specific failure reason**: Each fallback type produces its own audit status, enabling precise operational monitoring.
- **Thread-safe**: FallbackService is stateless (contextBuilder is immutable), so concurrent access is safe.
- **Event severity sorting**: Events in fallback summary are sorted by severity (high > medium > low) so the most important events appear first.

### Client Expectations

Clients consuming `GatewayResponse` should check the `Status` field:
- `"success"`: Full AI analysis is available with explanations and recommendations
- `"provider_error"`, `"budget_limited"`, `"rate_limited"`, `"invalid_response"`: Only deterministic summary is available. No `EventExplanations` or `Recommendations` are provided. The client should display the summary text as-is and may optionally show the status to the user.

## Phase 7.6 — HTTP API Integration

Phase 7.6 composes the provider-neutral AI pipeline and exposes it via the REST API.

### Architecture

The full pipeline composition in the HTTP layer:

```
POST /api/v1/sessions/{sessionId}/analyze
  └── analyzeHandler (validates JSON, dispatches to AIService)
        └── AuditedService.Analyze()  ← decorator: logs audit records
              └── FallbackService.Analyze()  ← decorator: handles provider errors
                    └── Service.Analyze()  ← concrete pipeline
                          ├── GatewayRequest validation
                          ├── PromptBuilder.Build() → system + user prompts
                          ├── Controller.PreCheck() → budget + rate limit
                          ├── Controller.ExecuteWithRetries() → calls adapter
                          └── ProviderAdapter.Analyze()  ← pluggable (fake for now)
```

### Service (internal/ai/service.go)

`Service` is the concrete `AIService` implementation that wires:
- `PromptBuilder` to build system/user prompts from the `GatewayRequest`
- `Controller` for budget pre-checks, rate limiting, retries, and usage accounting
- `ProviderAdapter` for the actual provider call

Construction:
```go
adapter := &myProviderAdapter{}
controller := &ai.Controller{
    Budget:        ai.DefaultTokenBudget(),
    Retry:         ai.DefaultRetryPolicy(),
    RateLimiter:   ai.NewRateLimiter(10, 5),
    Account:       ai.NewUsageAccount(),
    CostEstimator: ai.DefaultCostEstimator(),
}
svc := ai.NewService(adapter, controller).WithModel("gpt-4o-mini")
auditedSvc := ai.NewAuditedService(ai.NewFallbackService(svc), memoryAudit, nil)
```

The model and temperature are configurable via `WithModel()` and `WithTemperature()` builder methods.

### Endpoint

`POST /api/v1/sessions/{sessionId}/analyze`

Request body:
```json
{
    "input": { /* ConsumerInput: contractVersion, session, events, safety */ },
    "mode": "engineer" | "coach"
}
```

Response (200): `GatewayResponse` JSON — summary, event explanations, recommendations, provider info, status.

Errors:
- 400: Standard error envelope for invalid requests, raw telemetry rejection, unsupported mode.
- 404 `session_not_found`: Session does not exist. Create one via `POST /api/v1/sessions` first.

### Route Registration

`routesWithAI()` registers all standard routes plus the analyze endpoint. It follows the same dependency injection pattern as `routesWithEventStore`.

### Client Expectations

Clients send structured events (ConsumerInput contract) and receive structured analysis (GatewayResponse). The `Status` field indicates success or one of the fallback conditions (`provider_error`, `budget_limited`, `rate_limited`, `invalid_response`).

### Provider Selection

See Phase 7.8: `ComposePipeline()` in `internal/ai/compose.go` selects the provider adapter based on `TELEMETRY_ONE_AI_PROVIDER`:

| Value | Adapter | Use Case |
|-------|---------|----------|
| `"fake"` (default) | `FakeProvider` | Local development, testing, no API key needed |
| `"openrouter"` | `OpenRouterAdapter` | Production with OpenRouter API key configured |

## OpenRouter Integration (Phase 7.7)

Phase 7.7 replaces the fake `ProviderAdapter` with a real `OpenRouterAdapter` that calls the OpenRouter API.

### Architecture

```
ProviderRequest → OpenRouterAdapter.Analyze()
                  │
                  ├── POST /api/v1/chat/completions (OpenAI-compatible)
                  ├── Headers: Authorization, Content-Type, HTTP-Referer (opt), X-OpenRouter-Title (opt)
                  ├── Map HTTP errors consistently:
                  │   ├── 401/403 → ErrProviderRejected (non-retryable)
                  │   ├── 400 → ErrProviderRejected (non-retryable)
                  │   ├── 429 → ErrProviderNotAvailable (retryable)
                  │   ├── 5xx → ErrProviderNotAvailable (retryable)
                  │   └── network/timeout → ErrProviderNotAvailable or ErrProviderTimeout (retryable)
                  ├── Parse response → ProviderResponse with Content, Model, FinishReason, Usage
                  └── Empty choices → ErrInvalidProviderResponse → fallback with StatusInvalidResponse
```

### Adapter

`internal/ai/openrouter.go`: `OpenRouterAdapter` implementing `ProviderAdapter`.

```go
type OpenRouterConfig struct {
    APIKey      string  // TELEMETRY_ONE_OPENROUTER_API_KEY
    BaseURL     string  // TELEMETRY_ONE_OPENROUTER_BASE_URL (default: https://openrouter.ai/api/v1)
    HTTPReferer string  // TELEMETRY_ONE_OPENROUTER_HTTP_REFERER (optional attribution)
    Title       string  // TELEMETRY_ONE_OPENROUTER_TITLE (optional attribution)
}
```

Construction:
```go
adapter := ai.NewOpenRouterAdapter(ai.OpenRouterConfig{
    APIKey:      cfg.OpenRouterAPIKey,
    BaseURL:     cfg.OpenRouterBaseURL,
    HTTPReferer: cfg.OpenRouterHTTPReferer,
    Title:       cfg.OpenRouterTitle,
})
```

### Provider Selection

`TELEMETRY_ONE_AI_PROVIDER` config env var selects the adapter:
- `"fake"` (default) — uses `fakeProvider` for development/testing
- `"openrouter"` — uses `OpenRouterAdapter`

Wiring in `internal/ai/compose.go` via `ComposePipeline()`:

```go
pipeCfg := ai.PipelineConfigFromConfig(cfg)
aiSvc := ai.ComposePipeline(pipeCfg, logger)
```

`ComposePipeline` builds the full pipeline: `Service(adapter, controller).WithModel(model)` → `FallbackService` → `AuditedService`. It selects the adapter based on `AIProvider`:

- `"openrouter"` → `NewOpenRouterAdapter(OpenRouterConfig{...})`
- default (including `"fake"`) → `FakeProvider{}`

The `FakeProvider` is a production-safe implementation that returns deterministic responses for local/testing use without requiring an API key.

### Pipeline Composition

```
ComposePipeline(pipeCfg, logger)
  └── select adapter (fake or OpenRouter)
  └── Controller with budget, retry, rate limiter, usage account, cost estimator
  └── Service(adapter, controller).WithModel(model)
  └── FallbackService(service)  ← decorator: handles provider errors gracefully
  └── AuditedService(fallback, memoryAudit, nil)  ← decorator: logs audit records
  └── returns AIService (ready for routes)
```

`routes()` in `internal/api/routes.go` builds the full pipeline by default. No special wiring in `main.go` is needed — the server just calls `routes(cfg, logger)` which already includes the AI pipeline.

### Configuration

| Env Var | Default | Description |
| --- | --- | --- |
| `TELEMETRY_ONE_AI_PROVIDER` | `"fake"` | Provider adapter: `"fake"` or `"openrouter"` |
| `TELEMETRY_ONE_OPENROUTER_API_KEY` | — | OpenRouter API key (required for openrouter provider) |
| `TELEMETRY_ONE_OPENROUTER_BASE_URL` | `https://openrouter.ai/api/v1` | Base URL for OpenRouter API |
| `TELEMETRY_ONE_OPENROUTER_HTTP_REFERER` | — | HTTP-Referer header (OpenRouter attribution) |
| `TELEMETRY_ONE_OPENROUTER_TITLE` | — | X-OpenRouter-Title header (OpenRouter attribution) |

### Credential Security

- API key is server-side only, never in Flutter or client bundles.
- No early validation of API key — empty key results in 401 from OpenRouter, mapped to `ErrProviderRejected` with deterministic fallback.

### Error Mapping

See `mapHTTPError()` in `openrouter.go`. All errors use package-level sentinels consistent with the retry policy in `controls.go`.

### Tests

All tests use `httptest.NewServer` — no live network calls, no real API key required.

## Boundary Rules

- Gateway receives ONLY structured Engineer events and derived metrics. No raw frames, positions, speeds, throttle/brake/steering, wheel speeds, fuel, or yaw.
- Track/layout/corner names come ONLY from catalog metadata.
- Unknown track/layout/corner states remain explicit (null).
- Provider credentials are backend-owned.
- The gateway does not ship in Flutter.
