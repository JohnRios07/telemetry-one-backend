# AI Gateway Controls (Phase 7.3)

Phase 7.3 adds provider-neutral cost controls, rate limiting, retries, usage accounting, and observability hooks to the AI gateway boundary.

## Architecture

Controls are composed in the `Controller` struct, which acts as a pre/post decorator around the `ProviderAdapter`:

```
GatewayRequest
     │
     ▼
Controller.Execute()
  ├── PreCheck() → TokenBudget + RateLimit
  ├── ExecuteWithRetries() → RetryPolicy
  │     └── ProviderAdapter.Analyze()
  └── RecordResult() → UsageAccount + ObservabilityHooks
```

## Design Decisions

### All controls are provider-neutral
No type references OpenRouter, OpenAI, or any specific provider. The `Controller` works with `ProviderRequest`/`ProviderResponse` (existing types) and adds cross-cutting concerns.

### Budget is checked BEFORE the provider call
- `MaxPromptChars`: total characters in system + user prompt before sending
- `MaxEventsPerRequest`: number of events before sending (from `ConsumerInput`)
- `MaxCompletionTokens`: passed to `ProviderRequest.MaxTokens` (adapter responsibility)
- If budget is exceeded → `ErrBudgetExceeded` → adapter is never called

### Rate limiter is per-session token bucket
- `RateLimiter` uses a token bucket algorithm per `sessionID`
- Configurable rate (`tokens/sec`) and burst
- New sessions start with a full bucket
- Returns `retryAfter` for the caller to wait
- No rate limiter → no rate checks (zero-value safe)

### Retry policy is exponential backoff with jitter-free cap
- `IsRetryable()`: returns true for `ErrProviderNotAvailable` and `ErrProviderTimeout`
- Returns false for: `ErrProviderRejected` (4xx equivalent), `context.Canceled`, `context.DeadlineExceeded`, and unknown errors
- Backoff: `baseBackoffMs * 2^(attempt-1)` capped at `maxBackoffMs`
- At least 1 attempt is always made (even if `MaxAttempts = 0` in config)

### Usage accounting is in-memory, per-session
- `UsageAccount` accumulates `TotalRequests`, `TotalPromptTokens`, `TotalCompletionTokens`, `TotalCost`
- Cost estimation uses configurable per-1K-token rates
- Thread-safe via `sync.Mutex`
- Records only on success (errors are not accounted)

### Observability hooks are optional callbacks
- `AfterRequest`: called on both success and error; includes duration, tokens, error
- `BudgetExceeded`: called when PreCheck rejects a request
- `RateLimited`: called when rate limiter blocks
- `RetryAttempt`: called on each retry (not on the first attempt)
- All hook fields are function pointers; nil = noop, zero-value `ObservabilityHooks{}` is safe

### Controller composes all controls
- `Execute()` is the single entry point: PreCheck → ExecuteWithRetries → RecordResult
- Individual methods (`PreCheck`, `ExecuteWithRetries`, `RecordResult`) are available for custom composition
- `Validate()` checks both Budget and Retry for required fields

## Types

All types in `internal/ai/controls.go`:

| Type | Purpose |
|------|---------|
| `TokenBudget` | Max prompt chars, completion tokens, events per request |
| `RetryPolicy` | Max attempts, exponential backoff config, retryable error check |
| `RateLimiter` | Per-session token bucket rate limiter |
| `CostEstimator` | Token-to-cost calculation with per-1K rates |
| `SessionUsage` | Accumulated usage for one session |
| `UsageAccount` | Thread-safe in-memory usage store |
| `ObservabilityHooks` | Optional callback functions for observability |
| `Controller` | Orchestrator: PreCheck, ExecuteWithRetries, RecordResult, Execute |

## Errors

| Error | Cause |
|-------|-------|
| `ErrBudgetExceeded` | Prompt chars or event count exceeds `TokenBudget` |
| `ErrRateLimited` | `RateLimiter.Allow()` returned false |
| `ErrRetryExhausted` | All retry attempts failed with retryable errors |

## Configuration

Environment variables in `internal/config/config.go`:

| Variable | Default | Description |
|----------|---------|-------------|
| `TELEMETRY_ONE_AI_MAX_PROMPT_CHARS` | 40000 | Max prompt characters |
| `TELEMETRY_ONE_AI_MAX_COMPLETION_TOKENS` | 2000 | Max completion tokens requested |
| `TELEMETRY_ONE_AI_MAX_EVENTS_PER_REQUEST` | 50 | Max events per request |
| `TELEMETRY_ONE_AI_RETRY_MAX_ATTEMPTS` | 3 | Max retry attempts |
| `TELEMETRY_ONE_AI_RETRY_BASE_BACKOFF_MS` | 1000 | Base backoff in milliseconds |
| `TELEMETRY_ONE_AI_RETRY_MAX_BACKOFF_MS` | 10000 | Max backoff in milliseconds |
| `TELEMETRY_ONE_AI_RATE_PER_SECOND` | 10.0 | Rate limit tokens per second per session |
| `TELEMETRY_ONE_AI_RATE_BURST` | 5 | Rate limit burst size |
| `TELEMETRY_ONE_AI_COST_PER_PROMPT_TOKEN` | 0.00015 | Cost per 1K prompt tokens |
| `TELEMETRY_ONE_AI_COST_PER_COMPLETION_TOKEN` | 0.00060 | Cost per 1K completion tokens |

## Boundary Rules

- Controls never access raw telemetry
- Token budget is checked before any provider call — no cost incurred on budget violation
- Rate limiter uses `sessionID` only; no PII or telemetry data involved
- `RetryPolicy.IsRetryable` uses sentinel errors only; no error content inspection
- Usage accounting records tokens and cost only on successful provider responses
- All hooks receive structured session/mode/duration data; never raw frames or positions
