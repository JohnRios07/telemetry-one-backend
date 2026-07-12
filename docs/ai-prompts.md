# AI Prompt Templates (Phase 7.2)

Phase 7.2 defines provider-neutral, deterministic prompt templates for Engineer and Coach modes. Prompts are constructed from structured `ContextSummary` (built by `ContextBuilder`) only — never raw telemetry frames.

## Architecture

```
GatewayRequest (ConsumerInput + Mode)
        │
        ▼
PromptBuilder
  ├── Build(req) → PromptSet
  │      ├── Engineer: buildEngineer() → system + user prompt
  │      └── Coach: buildCoach() → system + user prompt
  │
PromptSet { SystemPrompt, UserPrompt, Mode }
        │
        ▼
ProviderAdapter.Analyze(ctx, ProviderRequest)
  (Phase 7.3+ — network call to OpenRouter / etc.)
```

## Prompt Contract

### `PromptSet` (internal/ai/prompts.go)

| Field | Type | Purpose |
|-------|------|---------|
| `SystemPrompt` | string | Mode-specific system/role prompt with constraints |
| `UserPrompt` | string | Formatted context + instructions for the AI |
| `Mode` | string | `"engineer"` or `"coach"` |

### `PromptBuilder` (internal/ai/prompts.go)

- Wraps `ContextBuilder` for user prompt generation.
- `Build(GatewayRequest)` dispatches to mode-specific builder based on `req.Mode`.
- Unknown mode returns empty `PromptSet`.

## Engineer Mode

**Role:** Technical driving data analyst.
**Tone:** Technical, objective, analytical.

### System prompt includes:
- Role: "Telemetry One Engineer"
- Allowed input kinds (from `SafetyMetadata.AllowedInputKinds`)
- Redaction policy (from `SafetyMetadata.RedactionPolicy`)
- Constraints:
  1. Only reference catalog-provided track/layout/corner names.
  2. Unknown catalog refs must stay explicit — never invent names.
  3. Analysis based solely on structured events and derived metric evidence.
  4. No inferring raw telemetry data not provided.
  5. No fabricating events, metrics, or driving details.
- Caller constraints appended verbatim if present.
- Expected output: per-event explanations with relevance scores (0.0–1.0), session summary, 2–3 recommendations.

### User prompt:
- "Analyze the following driving session data:"
- Context text from `ContextBuilder.BuildPromptSummary()`
- Instruction to produce structured output

## Coach Mode

**Role:** Expert driving instructor.
**Tone:** Constructive, encouraging, actionable.

### System prompt includes:
- Role: "Telemetry One Coach"
- Allowed input kinds, redaction policy, constraints (same structure as Engineer)
- Additional constraint: focus on actionable advice
- Additional constraint: prioritize most frequent/severe events
- Expected output: per-event coaching explanations with relevance scores, encouraging summary, 2–3 actionable tips.

### User prompt:
- "Review the following driving session and provide coaching feedback:"
- Context text from `ContextBuilder.BuildPromptSummary()`
- Instruction to produce structured coaching output

## Design Decisions

### Provider-Neutral
- Prompt templates contain no provider-specific formatting (no OpenAI chatml, no Claude XML tags).
- `ProviderAdapter` (Phase 7.3+) wraps prompts into provider-specific request format.
- Temperature, max tokens, model selection are set by the adapter, not the prompt builder.

### Deterministic
- `PromptBuilder.Build()` is a pure function of `GatewayRequest`.
- Same input always produces identical prompts — no randomness, no branching logic based on event content.

### Safety in System Prompts
- Every system prompt includes allowed input kinds, redaction policy, and constraints.
- The AI is explicitly instructed not to infer raw telemetry, not to invent catalog names, and to base analysis only on provided events.
- Constraint text is abstract ("do not assume or infer raw telemetry data") rather than listing forbidden field names.

### Catalog Name Handling
- Track/layout/corner names come from catalog only (via `ContextBuilder` which extracts `CatalogRef.Name`).
- References to "unknown" are explicit when catalog refs are nil.
- System prompts reinforce: "do not invent a name."

### Response Contract
- Both modes expect a `GatewayResponse`-compatible shape:
  - `Summary`: overall session analysis
  - `EventExplanations[]`: per-event explanation with relevance score
  - `Recommendations[]`: 2–3 actionable items
- The exact structured parsing is handled by `ProviderAdapter` (Phase 7.3+).

## Files

| File | Purpose |
|------|---------|
| `internal/ai/prompts.go` | `PromptBuilder`, `PromptSet`, prompt construction |
| `internal/ai/prompts_test.go` | 24 tests: constraints, mode differentiation, no raw telemetry, deterministic, unknown refs |

## Future Phases

| Phase | Scope |
|-------|-------|
| 7.3 | `ProviderAdapter` (OpenRouter) — wraps PromptSet into provider request, parses response into GatewayResponse |
| 7.4 | Traceability — response-to-event source audit logging |
| 7.5 | Degraded behavior — fallback when provider fails |

## Boundary Rules

- Prompts contain only structured event summaries, derived metrics, session context, catalog refs, and constraints.
- Prompts never contain raw telemetry fields: positionX/Y/Z, speedMps, throttle, brake, steering, wheel speeds, fuel, yaw, frames.
- Track/layout/corner names come only from catalog metadata (via `ContextBuilder`).
- Unknown catalog refs remain explicit ("unknown" in formatted text).
- Prompt templates are deterministic — no prompt engineering by event content.
