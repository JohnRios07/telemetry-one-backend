# Telemetry One Backend API

Initial API contract for the backend scaffold. Frame batch ingest is functional at MVP level: payloads are validated, retained in a bounded in-memory buffer, and acknowledged. Engineer events can be stored in a bounded in-memory repository and exposed by session with filters. DB-backed frame/event persistence, frame-to-event processing, and live streams are intentionally deferred to later phases. Flutter-specific integration mapping, batching, retry behavior, and planned live/analysis DTOs are documented in `docs/flutter-contract.md`.

All error responses use the same envelope:

```json
{
  "error": {
    "code": "bad_request",
    "message": "platform is required"
  }
}
```

## API Version Marker

Current implemented `/api/v1` routes are a compatibility bridge for the frozen V2 response contract. Successful JSON object responses include the additive top-level marker:

```json
{
  "apiVersion": "telemetry-one.api.v2"
}
```

This slice does not add `/api/v2`, remove `/api/v1`, or require a `{ "data": ... }` envelope. Existing error envelopes remain unchanged and do not include `apiVersion`; non-object payloads are not marker-mutated. Current ID/enum constraints remain the active contract: backend-owned session IDs use the `session_` prefix, engineer events use `telemetry-one.engineer-event.v1`, and stable machine enum/code fields such as ingest rejection codes, track detection statuses/reasons, event types/severities, and advice statuses must be treated as closed to the documented values until a later enum cleanup slice. Implemented `/api/v1` routes now also include `GET /api/v1/catalog/track-layouts` and `PUT /api/v1/sessions/{sessionId}/track-layout`.

Validation failures may include stable rejection details for Flutter retry and user-facing diagnostics:

```json
{
  "error": {
    "code": "bad_request",
    "message": "frames[0]: throttle must be between 0 and 1",
    "details": {
      "rejectionCode": "invalid_throttle",
      "category": "frame",
      "field": "throttle",
      "frameIndex": 0
    }
  }
}
```

## Health

```http
GET /api/v1/health
```

Response:

```json
{
  "apiVersion": "telemetry-one.api.v2",
  "status": "ok",
  "env": "development",
  "time": "2026-07-11T00:00:00Z"
}
```

## Settings Bootstrap

```http
GET /api/v1/settings/bootstrap
```

Returns a whitelist-only bootstrap payload for client settings screens. The response is read-only, versioned with `apiVersion: telemetry-one.api.v2`, and only exposes safe defaults, client hints, limits, and capability flags.

Response:

```json
{
  "apiVersion": "telemetry-one.api.v2",
  "bootstrap": {
    "clientHints": {
      "alias": "",
      "units": "metric"
    },
    "limits": {
      "maxBatchFrames": 600,
      "retainedFramesPerSession": 12000
    },
    "capabilities": {
      "readOnly": true,
      "acceptsPartialIngest": true,
      "writeApi": false
    }
  }
}
```

`clientHints` are advisory only and are not persisted by the backend. `limits` reflect backend-safe operational bounds. `capabilities` describe what the backend contract supports, not user-owned settings.

## Catalog Track Layouts

```http
GET /api/v1/catalog/track-layouts
```

Returns sourced-only track/layout summaries for the runtime catalog. The payload includes stable ids, display names, provenance sources, and layout lengths. It does not include geometry, sectors, corners, or telemetry-derived names.

### Example

```json
{
  "apiVersion": "telemetry-one.api.v2",
  "catalogVersion": "telemetry-one.track-catalog.v1",
  "tracks": [
    {
      "id": "gt7_watkins_glen_international",
      "name": "Watkins Glen International",
      "layouts": [
        {
          "id": "gt7_layout_1240",
          "name": "Watkins Glen Long Course",
          "lengthMeters": 5423
        }
      ]
    }
  ]
}
```

## Sessions

```http
POST /api/v1/sessions
```

Creates the contract for a driving session. The current implementation validates the request and returns `501 not_implemented` until persistence lands in Phase 1.4.

Request:

```json
{
  "source": "flutter",
  "game": "gt7",
  "platform": "ps5",
  "driverAlias": "alex",
  "trackId": "monza_gp",
  "startedUnixMs": 1720656000000
}
```

Future success response:

```json
{
  "apiVersion": "telemetry-one.api.v2",
  "session": {
    "id": "session_01j2example",
    "startedAt": "2026-07-11T00:00:00Z",
    "trackId": "monza_gp"
  }
}
```

Required fields:

- `source`: client/source name, for example `flutter`.
- `game`: game identifier, for example `gt7`.
- `platform`: platform identifier, for example `ps5`.
- `startedUnixMs`: session start timestamp in Unix milliseconds.

`trackId` is optional because GT7 UDP does not provide authoritative track names. When present, it must come from Telemetry One catalog metadata or explicit user selection, not from raw UDP fields. The effective layout can later be overridden through `PUT /api/v1/sessions/{sessionId}/track-layout`; reads surface that effective layout as `layoutId` while preserving detected provenance in `detectedLayoutId`.

```http
PUT /api/v1/sessions/{sessionId}/track-layout
```

Request:

```json
{
  "trackId": "gt7_watkins_glen_international",
  "layoutId": "gt7_layout_1240"
}
```

The manual pair must exist in the sourced catalog and the layout must belong to the selected track. The backend persists the pair on the session record and returns the standard versioned session response.

When a session response can resolve a concrete layout, the backend may also include `trackCapabilities` with explicit per-capability states for Flutter to render.

### Session Validation

All V2 endpoints validate `{sessionId}` against the backend session repository:

| Endpoint | Active required | Nonexistent | Finished |
|---|---|---|---|
| `POST /api/v1/sessions/{sessionId}/frames` | Yes | 404 `session_not_found` | 409 `session_finished` |
| `GET /api/v1/sessions/{sessionId}/track` | No | 404 `session_not_found` | Allowed |
| `GET /api/v1/sessions/{sessionId}/events` | No | 404 `session_not_found` | Allowed |
| `POST /api/v1/sessions/{sessionId}/analyze` | No | 404 `session_not_found` | Allowed |
| `POST /api/v1/sessions/{sessionId}/race-engineer/advice` | No | 404 `session_not_found` | Allowed |

A missing or empty `sessionId` returns `400 invalid_session_id`.

The backend does NOT auto-create sessions from frame ingest. Auto-creation would weaken session lifecycle invariants by accepting arbitrary local session IDs, bypassing required metadata (`source`, `game`, `platform`, `startedUnixMs`), and making it impossible to distinguish active from finished sessions. Clients MUST call `POST /api/v1/sessions` first and use the returned backend session ID.

Client fallback: if `POST /api/v1/sessions/{sessionId}/frames` returns `session_not_found`, the client should re-establish session alignment by creating a new session and flushing buffered frames under the new ID. Retrying the same local ID indefinitely will not succeed.

## Telemetry Frames

```http
POST /api/v1/sessions/{sessionId}/frames
```

Accepts normalized frame batches sent by Flutter. The current implementation validates and normalizes the whole batch, stores accepted frames in a bounded in-memory per-session buffer, then runs a small deterministic frame-event producer and stores accepted events in the in-memory event repository. Distance/corner processing is still deferred to later phases.

Request:

```json
{
  "sessionId": "session_01j2example",
  "frames": [
    {
      "timestampUnixMs": 1720656000123,
      "speedMps": 58.33,
      "rpm": 7100,
      "gear": 4,
      "throttle": 0.82,
      "brake": 0,
      "steering": -0.12,
      "fuelLiters": 38.4,
      "positionX": 123.4,
      "positionY": 5.6,
      "positionZ": 789.1,
      "yawRadians": 1.57,
      "yawRate": 0.03,
      "wheelSpeedFL": 58.1,
      "wheelSpeedFR": 58.2,
      "wheelSpeedRL": 58.4,
      "wheelSpeedRR": 58.3,
      "lapNumber": 2,
      "currentLapMs": 81234,
      "lastLapMs": 91345,
      "bestLapMs": 90210,
      "isOnTrack": true
    }
  ]
}
```

Validation and ingest behavior:

- Session must exist and be active (see Session Validation above). Returns 404 `session_not_found` if the session does not exist or 409 `session_finished` if the session is already finished.
- `sessionId` is required and must match `{sessionId}` when included in the body.
- `frames` must contain at least one frame and no more than 600 frames.
- `timestampUnixMs` must be greater than zero.
- `speedMps`, `rpm`, `fuelLiters`, wheel speeds, lap times, and position fields must be finite. Values that cannot be represented as real numbers are rejected.
- `speedMps`, `rpm`, `fuelLiters`, wheel speeds, lap numbers, and lap times must be zero or greater.
- `throttle` and `brake` must be normalized from `0.0` to `1.0`; invalid values are rejected, not clamped.
- `lapNumber` must be zero or greater.
- `yawRadians`, `yawRate`, wheel speeds, `lastLapMs`, and `bestLapMs` are optional. Missing optional fields remain unknown and must not be invented.
- Frames must be ordered by strictly increasing `timestampUnixMs` within a batch.
- `lapNumber` must not decrease within a batch.
- `currentLapMs` must not decrease while `lapNumber` stays the same. It may reset when `lapNumber` increases.
- Invalid frames are rejected individually; valid frames in the same batch are still accepted.
- When all frames are invalid, the batch status is `rejected` and no frames are stored.
- When some frames are valid and some invalid, the batch status is `partial`: valid frames are accepted and stored, invalid frames are counted and summarized.
- Batch-level errors (empty frames, batch too large) still reject the entire batch with the standard error envelope (400) and `rejectionSummary` is absent.

Retention behavior:

- Accepted normalized frames are retained in memory per backend process and per `sessionId`.
- The per-session buffer keeps the most recent `TELEMETRY_ONE_RETAINED_FRAMES_PER_SESSION` frames and evicts the oldest frames when the bound is exceeded.
- The default bound is `12000` frames per session, enough for several minutes of MVP live analysis at common telemetry rates without committing to long-term raw-frame storage.
- Retention preserves accepted append order and keeps optional fields unknown when Flutter omits them.
- Rejected batches are never retained.
- The buffer is ephemeral: process restarts, horizontal scaling, and multi-instance routing can lose or split retained frames. That is intentional for the MVP so upcoming track, distance, and event engines can consume recent normalized frames without contradicting the current PostgreSQL schema, which intentionally stores sessions, catalog metadata, corners, and structured engineer events rather than raw telemetry frames.

Stable ingest rejection codes:

| Code | Category | Meaning |
| --- | --- | --- |
| `session_id_required` | `batch` | Request did not include or infer a session ID. |
| `session_id_mismatch` | `batch` | Body `sessionId` differs from the route `{sessionId}`. |
| `frames_empty` | `batch` | Batch contains no frames. |
| `batch_too_large` | `batch` | Batch contains more than 600 frames. |
| `invalid_timestamp` | `frame` | Frame timestamp is missing or not greater than zero. |
| `invalid_speed` | `frame` | Speed is negative or not finite. |
| `invalid_rpm` | `frame` | RPM is negative or not finite. |
| `invalid_throttle` | `frame` | Throttle is outside `0.0..1.0` or not finite. |
| `invalid_brake` | `frame` | Brake is outside `0.0..1.0` or not finite. |
| `invalid_steering` | `frame` | Steering is not finite. |
| `invalid_fuel` | `frame` | Fuel is negative or not finite. |
| `invalid_position` | `frame` | One or more position fields are not finite. |
| `invalid_yaw` | `frame` | Optional yaw field is present but not finite. |
| `invalid_wheel_speed` | `frame` | Optional wheel speed is present but negative or not finite. |
| `invalid_lap_number` | `frame` | Lap number is negative. |
| `invalid_lap_time` | `frame` | Lap time field is negative. |
| `non_monotonic_timestamp` | `consistency` | A frame timestamp is not greater than the previous frame timestamp. |
| `lap_number_regressed` | `consistency` | Lap number decreased compared with the previous frame. |
| `current_lap_time_regressed` | `consistency` | Current lap time decreased without a lap increment. |

See `docs/telemetry-normalization.md` for the canonical units, optional-field rules, and Flutter mapping notes.

Flutter `TelemetryData` alignment:

- `timestamp` maps to `timestampUnixMs`.
- `speedKmh` maps to `speedMps` by dividing by `3.6`.
- `fuelCurrentL` maps to `fuelLiters`.
- `currentLap`, `currentLapTime`, `lastLapTime`, and `bestLapTime` map to lap fields in milliseconds.
- `posX`, `posY`, and `posZ` map directly to position fields.
- Fields not currently present in Flutter's shared `TelemetryData` model, such as yaw and wheel speeds, remain optional in the backend contract because they are required by the reference spec and later deterministic engines. They must be omitted/null until Flutter maps real source values.

Success response (`202 Accepted`) — all frames accepted:

```json
{
  "sessionId": "session_01j2example",
  "receivedFrames": 1,
  "acceptedFrames": 1,
  "rejectedFrames": 0,
  "acceptedFromUnixMs": 1720656000123,
  "acceptedToUnixMs": 1720656000123,
  "status": "accepted"
}
```

Partial acceptance response (`202 Accepted`) — some frames rejected:

```json
{
  "sessionId": "session_01j2example",
  "receivedFrames": 10,
  "acceptedFrames": 8,
  "rejectedFrames": 2,
  "acceptedFromUnixMs": 1720656000123,
  "acceptedToUnixMs": 1720656001000,
  "status": "partial",
  "rejectionSummary": {
    "reasons": [
      {"code": "invalid_throttle", "count": 1},
      {"code": "invalid_speed", "count": 1}
    ]
  }
}
```

All-rejected response (`202 Accepted`) — every frame in the batch was invalid:

```json
{
  "sessionId": "session_01j2example",
  "receivedFrames": 5,
  "acceptedFrames": 0,
  "rejectedFrames": 5,
  "acceptedFromUnixMs": 0,
  "acceptedToUnixMs": 0,
  "status": "rejected",
  "rejectionSummary": {
    "reasons": [
      {"code": "invalid_timestamp", "count": 5}
    ]
  }
}
```

The accepted time range is calculated from the accepted frame timestamps (zeroed when none accepted). `rejectionSummary` is present only when `rejectedFrames > 0`. Reasons are grouped by stable rejection code with counts; at most all unique codes are returned, ordered by count descending then code ascending for deterministic output.

For successfully handled ingest requests (`202 Accepted`) with `rejectedFrames > 0`, the backend persists the same compact rejection summary best-effort for later session reporting. Diagnostic persistence failures are logged as warnings and do not change ingest status or accepted-frame persistence. Batch-level `400` validation errors are not persisted, and raw rejected frames are never stored.

The response intentionally uses a compact aggregated summary (`rejectionSummary`) rather than per-frame rejection details. This keeps the API response lightweight for production telemetry ingest. Per-frame diagnostics (including `frameIndex`, `category`, `field`) are available through the error envelope (400) for batch-level validation failures such as missing session ID, empty batch, or exceeding the batch size limit. For batch-level failures there is no `rejectionSummary` because the entire request is rejected as a single unit and the specific rejection detail is returned in the error envelope.

See `docs/ingest-performance.md` for expected frame rates, batch-size coverage, retention windows, current ingest benchmark coverage, and MVP limits.

## Engineer Events

```http
GET /api/v1/sessions/{sessionId}/events
```

Returns structured Engineer events already accepted by the backend for a session. The endpoint validates session existence (see Session Validation above); session must exist but may be finished. It exposes events that were created by deterministic code paths, validated against the Engineer event contract, deduplicated, and stored in the MVP in-memory event repository.

Engineer events are deterministic facts derived from validated analysis metrics and context. They are not free-form AI messages and they never include raw telemetry frames.

Phase 5.2 adds a pure deterministic rule engine in `internal/events`. It consumes per-corner analysis metrics, optional reference metrics, catalog references, lap/session context, and configurable rule thresholds. The ingest path also has a small frame-driven producer for lap regression and sustained off-track signals before corner analysis is available.

Phase 5.3 adds deterministic event deduplication/cooldown in `internal/events` before persistence or API exposure. Generated events pass through an explicit `EventDeduplicator` that returns accepted events plus one decision per input event, so suppressed duplicates are traceable and are not dropped silently.

Phase 5.4 adds an in-memory event store behind the event repository boundary. Append paths validate `EngineerEvent` before storing, reject raw telemetry metrics, and apply the same deduplication key/cooldown rules before events become visible through the API. Durable PostgreSQL persistence remains future work because the backend still has no runtime database driver or migration runner.

Query filters:

| Query parameter | Required | Meaning |
| --- | --- | --- |
| `lapNumber` | No | Return only events for a lap. Must be zero or greater. |
| `cornerId` | No | Return only events whose catalog corner reference ID matches. Names are never inferred. |
| `type` | No | Return only one supported event type. |

Storage behavior:

- Events are retained in memory per backend process with a default limit of `1000` stored events.
- The store preserves accepted append order and evicts oldest events when the bound is exceeded.
- The store is ephemeral: process restarts, horizontal scaling, and multi-instance routing can lose or split events.
- This is intentional for MVP exposure while DB runtime wiring is absent. The SQL migration already sketches durable `engineer_events`, but it has not yet been promoted to the stricter Phase 5.1 event contract.
- The endpoint returns only structured events and derived metric evidence. It never exposes raw telemetry frames or raw frame fields.

Current MVP event types:

| Type | Meaning | Supported evidence |
| --- | --- | --- |
| `early_braking` | Brake point occurred earlier than a validated reference/threshold. | Derived brake-point deltas and related corner metrics. |
| `late_braking` | Brake point occurred later than a validated reference/threshold. | Derived brake-point deltas and related speed metrics. |
| `late_throttle` | First throttle reapplication occurred later than a validated reference/threshold. | Derived throttle reapplication timing/distance. |
| `low_exit_speed` | Corner exit speed was below a validated reference/threshold. | Derived exit-speed metrics. |
| `inconsistent_corner` | Corner execution varied beyond a validated consistency threshold. | Derived variance/consistency metrics. |
| `lap_time_regression` | A completed lap was significantly slower than the session best lap or immediately previous completed lap. | Derived last-lap/best-lap metrics, or previous-lap pace-drop delta and threshold metrics. |
| `off_track_stint` | A sustained off-track stretch exceeded the minimum duration threshold. | Derived off-track duration, frame count, and threshold metrics. |

Reserved ideas such as `weak_corner`, `overdriving`, `line_deviation`, `understeer`, and `oversteer` are intentionally not part of the current MVP contract until the deterministic rule inputs exist.

Current deterministic rule coverage:

| Rule | Event | Baseline requirement | Conservative behavior |
| --- | --- | --- | --- |
| `late_throttle.v1` | `late_throttle` | None. Uses first throttle reapplication offset from corner analysis against configurable absolute thresholds. | No event if throttle reapplication is unavailable or below threshold. |
| `low_exit_speed.v1` | `low_exit_speed` | Required explicit reference exit speed. | No event if reference exit speed is missing/unavailable, current exit speed is unavailable, or delta is below threshold. |

Braking delta, line deviation, stability, and consistency rules are intentionally not emitted in this slice. They require reference/baseline inputs or derived metrics that are not yet part of the completed corner analysis contract. Missing evidence must produce no event rather than fake certainty.

Default rule thresholds are configurable at engine call-site level:

| Option | Default | Meaning |
| --- | --- | --- |
| `LateThrottleDelayLowMs` | `250` | Minimum first-throttle reapplication offset from corner entry to emit `late_throttle`. |
| `LateThrottleDelayMediumMs` | `500` | Medium severity threshold. |
| `LateThrottleDelayHighMs` | `750` | High severity threshold. |
| `LowExitSpeedDeltaLowKph` | `5` | Minimum reference-minus-actual exit speed delta to emit `low_exit_speed`. |
| `LowExitSpeedDeltaMediumKph` | `10` | Medium severity threshold. |
| `LowExitSpeedDeltaHighKph` | `15` | High severity threshold. |

Severities are closed: `low`, `medium`, and `high`.

Deduplication/cooldown behavior:

| Input | Included in stable key? | Reason |
| --- | --- | --- |
| `sessionId` | Yes | Avoids cross-session suppression. |
| `type` | Yes | Different event types in the same corner remain distinct. |
| `lapNumber` | Yes | The same issue on a later lap is accepted. |
| track/layout/corner catalog IDs | Yes | Same type in a different catalog context is accepted. Nil refs are represented as `<none>`. |
| `source.kind`, `source.ruleId`, `source.ruleVersion` | Yes | Different deterministic rules or rule versions stay independently traceable. |
| `eventId` | No | Event IDs can be regenerated and must not defeat deduplication. |
| `timestampUnixMs` | No | Timestamp drives cooldown expiry, but is not part of identity. |

The default cooldown window is `30000` ms and can be overridden through `DedupOptions.CooldownWindowMs` at the engine call site. A same-key event inside the window is suppressed with reason `duplicate_inside_cooldown`; an out-of-order same-key event inside the window is suppressed with reason `out_of_order_inside_cooldown`; a same-key event at or beyond the window boundary is accepted with reason `cooldown_expired`. The decision also carries the previous event ID and timestamp when a previous same-key event exists.

Catalog references are nullable. Track, layout, and corner names may only appear when they come from Telemetry One catalog metadata. If a selected catalog entity is unknown, the reference is `null`; if only an ID is safe to expose, `displayStrategy` must be `id_only` and `name` must be `null`. The backend must not invent GT7 track, layout, or corner names from UDP telemetry.

Success response:

```json
{
  "sessionId": "session_01j2example",
  "events": [
    {
      "eventId": "event_01j2example",
      "sessionId": "session_01j2example",
      "version": "telemetry-one.engineer-event.v1",
      "type": "late_throttle",
      "severity": "medium",
      "confidence": 0.82,
      "timestampUnixMs": 1720656012345,
      "timeRange": {
        "startUnixMs": 1720656012000,
        "endUnixMs": 1720656012345
      },
      "lapNumber": 2,
      "track": {
        "id": "gt7_watkins_glen_international",
        "name": "Watkins Glen International",
        "displayStrategy": "catalog_name"
      },
      "layout": {
        "id": "gt7_watkins_glen_long_course",
        "name": "Long Course",
        "displayStrategy": "catalog_name"
      },
      "corner": null,
      "metrics": [
        {
          "name": "throttleReapplicationDeltaMs",
          "value": 320,
          "unit": "ms",
          "status": "available",
          "role": "delta"
        }
      ],
      "source": {
        "kind": "deterministic_rule",
        "ruleId": "late_throttle.v1",
        "ruleVersion": "v1"
      }
    }
  ]
}
```

AI consumers must use structured events and derived metrics only. Raw telemetry frame fields such as `positionX`, `positionY`, `positionZ`, `speedMps`, `throttle`, `brake`, `steering`, and wheel speeds are not part of the event or AI contract. The provider-neutral AI input schema is documented in `docs/ai-consumer-contract.md` and implemented by `internal/ai.ConsumerInput`; OpenRouter/network integration is intentionally deferred to Phase 7.

## Live Race Engineer Advice

```http
POST /api/v1/sessions/{sessionId}/race-engineer/advice
```

Returns short textual Race Engineer advice for an existing session by selecting stored structured `engineer_events` and building the AI input on the backend. The request never accepts raw telemetry frames, prompt text, provider names, model names, API keys, or provider configuration. Unknown JSON fields are rejected with the standard `400 bad_request` envelope.

Request body is optional:

```json
{
  "sinceUnixMs": 1720656000000,
  "maxEvents": 5
}
```

Rules:

- `sinceUnixMs` is optional. When present, it must be greater than zero and filters events with `timestampUnixMs >= sinceUnixMs`.
- `maxEvents` is optional, defaults to `5`, and is clamped to `1..10`.
- The endpoint selects the newest matching events up to `maxEvents`, preserving ascending timestamp order for backend AI input.
- If no events match, the endpoint returns `200 no_events` and does not call the AI provider.
- If events match, the endpoint calls the existing backend AI pipeline (`FakeProvider` by default, OpenRouter only via server environment) and returns safe provider metadata without secrets.

Success response:

```json
{
  "sessionId": "session_01j2example",
  "status": "success",
  "message": "Brake earlier into the bus stop and focus on throttle timing on corner exit.",
  "referencedEvents": ["event_01j2example"],
  "window": {
    "sinceUnixMs": 1720656000000,
    "maxEvents": 5,
    "selectedEventCount": 1,
    "fromUnixMs": 1720656012345,
    "toUnixMs": 1720656012345,
    "providerCalled": true
  },
  "providerInfo": {
    "model": "gpt-4o-mini",
    "finishReason": "stop",
    "usage": {"promptTokens": 10, "completionTokens": 20, "totalTokens": 30}
  },
  "generatedAtUnixMs": 1720656020000
}
```

No-events response:

```json
{
  "sessionId": "session_01j2example",
  "status": "no_events",
  "message": "No race engineer events are available for the selected window yet. Keep driving and request advice again after new events are detected.",
  "referencedEvents": [],
  "window": {
    "sinceUnixMs": 1720656000000,
    "maxEvents": 5,
    "selectedEventCount": 0,
    "fromUnixMs": 0,
    "toUnixMs": 0,
    "providerCalled": false
  },
  "providerInfo": {
    "model": "",
    "finishReason": "",
    "usage": {"promptTokens": 0, "completionTokens": 0, "totalTokens": 0}
  },
  "generatedAtUnixMs": 1720656020000
}
```

AI failure statuses returned by the existing gateway fallback path (`provider_error`, `budget_limited`, `rate_limited`, `invalid_response`) are surfaced in the same response shape with `providerCalled: true` and deterministic safe message content.

## Detect Session Track

```http
GET /api/v1/sessions/{sessionId}/track
```

Runs the deterministic track detector over retained frames for the session and the official GT7 seed catalog. The endpoint validates session existence (see Session Validation above); session must exist but may be finished. The detector is conservative: it only uses observed completed lap length, requires enough retained frames, and returns explicit fallback states when evidence is insufficient, weak, absent from the catalog, or ambiguous.

Stable statuses:

| Status | Meaning | Client behavior |
| --- | --- | --- |
| `pending` | Detection needs more temporal evidence, usually more frames or a completed lap. | Keep collecting/sending frames and do not unlock track-dependent analysis yet. |
| `detected` | One catalog layout matched above the minimum confidence. | Use `trackId`/`layoutId` and catalog-owned names for downstream state. |
| `low_confidence` | A candidate exists but is below the configured acceptance threshold. | Ask for manual confirmation/selection before track-dependent analysis. |
| `ambiguous` | Multiple catalog layouts are similarly plausible. | Ask for manual selection; do not auto-select the best candidate. |
| `unknown` | Evidence was sufficient to evaluate but no catalog layout matched. | Ask for manual selection or keep the session track unknown. |

Stable reasons include `insufficient_data`, `no_completed_lap`, `no_catalog_match`, `ambiguous_length`, `low_confidence`, and `length_match`. `nextAction` is a machine-readable hint for Flutter and future event engines: `collect_more_frames`, `wait_for_completed_lap`, `prompt_manual_track_selection`, or `use_detected_catalog_layout`.

`trackId`, `layoutId`, `trackName`, and `layoutName` are nullable. They are non-null only when `status` is `detected`; fallback states may include candidate details in `evidence`, but they do not select a track/layout. Selected display names are emitted only from catalog metadata with complete provenance sources; GT7 UDP telemetry and length heuristics never provide display names.

Example detected response:

```json
{
  "status": "detected",
  "trackId": "gt7_watkins_glen_international",
  "layoutId": "gt7_watkins_glen_long_course",
  "trackName": "Watkins Glen International",
  "layoutName": "Watkins Glen Long Course",
  "confidence": 0.85,
  "observedLengthMeters": 5423,
  "reasons": ["length_match"],
  "evidence": [
    {
      "reason": "length_match",
      "message": "observed completed lap length matched one catalog layout within tolerance",
      "trackId": "gt7_watkins_glen_international",
      "layoutId": "gt7_watkins_glen_long_course",
      "catalogMeters": 5423,
      "observedMeters": 5423,
      "confidence": 0.85
    }
  ],
  "nextAction": "use_detected_catalog_layout"
}
```

Example pending response:

```json
{
  "status": "pending",
  "trackId": null,
  "layoutId": null,
  "trackName": null,
  "layoutName": null,
  "confidence": 0,
  "reasons": ["insufficient_data"],
  "evidence": [
    {
      "reason": "insufficient_data",
      "message": "not enough retained frames to detect a completed lap"
    }
  ],
  "nextAction": "collect_more_frames"
}
```

Example unknown response after a completed lap with no catalog match:

```json
{
  "status": "unknown",
  "trackId": null,
  "layoutId": null,
  "trackName": null,
  "layoutName": null,
  "confidence": 0,
  "observedLengthMeters": 7000,
  "reasons": ["no_catalog_match"],
  "evidence": [
    {
      "reason": "no_catalog_match",
      "message": "observed lap length is outside catalog tolerance",
      "observedMeters": 7000
    }
  ],
  "nextAction": "prompt_manual_track_selection"
}
```

The endpoint does not infer official geometry, sectors, corners, or names from telemetry. Names appear only when a provenance-backed catalog layout is matched. The current official seed has no sourced sector boundaries or corner-name catalog, so this endpoint does not emit sector or corner names.

When a detected layout is matched, the response also includes a `capabilities` object with per-capability `{state, reason?}` entries. Layouts without runtime-approved geometry keep geometry-dependent capabilities explicitly `unavailable`.

## Admin Ingest Stats

```http
GET /api/v1/admin/ingest-stats
```

Returns operational stats about the ingest pipeline. Available in both memory and Postgres modes; Postgres mode returns all aggregate fields, memory mode omits batch, event, and audit stats (marked as absent/null).

Query parameters are optional. Omitted values use the defaults below. Present values must parse as integers and stay within the documented range; invalid or out-of-range values return `400 bad_request`.

### Authorization

If `TELEMETRY_ONE_ADMIN_TOKEN` is set, this endpoint requires `Authorization: Bearer <token>`. If unset, the endpoint is open.

| `TELEMETRY_ONE_ADMIN_TOKEN` | Behavior |
|---|---|
| Unset (empty) | Endpoint open — no auth required |
| Set | Requires `Authorization: Bearer <token>` |

Unauthorized responses use the standard error envelope with status 401 and code `unauthorized`.

### Query Parameters

| Parameter | Default | Range | Description |
|---|---|---|---|
| `limit` | `10` | `1..100` | Number of recent sessions to include |
| `days` | `7` | `1..90` | Number of past days for daily aggregates |

### Response

```json
{
  "mode": "postgres",
  "totals": {
    "sessions": 42,
    "activeSessions": 3,
    "finishedSessions": 39,
    "frameBatches": 156,
    "persistedFrames": 12480,
    "engineerEvents": 28,
    "aiAuditLogs": 12
  },
  "recentSessions": [
    {
      "id": "session_abc",
      "startedAt": "2026-07-14T10:00:00Z",
      "endedAt": "2026-07-14T12:30:00Z",
      "durationMs": 9000000,
      "frameBatches": 5,
      "persistedFrames": 400,
      "status": "finished"
    }
  ],
  "daily": [
    {"date": "2026-07-14", "sessions": 10, "frameBatches": 40, "persistedFrames": 3200}
  ]
}
```

In memory mode, `mode` is `"memory"` and unavailable aggregate fields (`frameBatches`, `engineerEvents`, `aiAuditLogs`) are omitted. The endpoint does NOT aggregate rejection diagnostics; rejection totals/reasons are exposed only through session list/detail APIs.

## Session History And Summary

### List Sessions

```http
GET /api/v1/sessions
```

Returns a paginated list of sessions with aggregate frame, batch, event, and rejected-frame counts. Available in both memory and Postgres modes.

No auth required. Detailed rejection reasons are intentionally not included in list responses.

Query parameters are optional. Omitted values use the defaults below. Present values must parse as integers and stay within the documented range; invalid or out-of-range values return `400 bad_request`.

#### Query Parameters

| Parameter | Default | Range | Description |
|---|---|---|---|
| `limit` | `20` | `1..100` | Number of sessions to return. |

#### Response

```json
{
  "sessions": [
    {
      "id": "session_01j2example",
      "source": "flutter",
      "game": "gt7",
      "platform": "ps5",
      "driverAlias": "alex",
      "trackId": "gt7_watkins_glen_international",
      "status": "finished",
      "startedAt": "2026-07-14T10:00:00Z",
      "endedAt": "2026-07-14T12:30:00Z",
      "durationMs": 9000000,
      "frameBatches": 5,
      "persistedFrames": 400,
      "rejectedFrames": 7,
      "eventCount": 3,
      "layoutId": "gt7_layout_1240",
      "detectedTrackId": "gt7_watkins_glen_international",
      "detectedLayoutId": "gt7_layout_1240"
    }
  ]
}
```

`rejectedFrames` is the per-session total from persisted successful-ingest diagnostics. It is `0` when no diagnostics exist. `layoutId` is the effective session layout: it prefers the manual override when present and otherwise falls back to the detected layout. `detectedTrackId` and `detectedLayoutId` remain provenance/history from detection.

### Session Summary

```http
GET /api/v1/sessions/{sessionId}/summary
```

Returns aggregate counts and derived metrics for a single session. Session must exist; returns `404 session_not_found` otherwise.

#### Response

```json
{
  "session": {
    "id": "session_01j2example",
    "source": "flutter",
    "game": "gt7",
    "platform": "ps5",
    "driverAlias": "alex",
    "trackId": "gt7_watkins_glen_international",
    "status": "finished",
    "startedAt": "2026-07-14T10:00:00Z",
    "endedAt": "2026-07-14T12:30:00Z",
    "durationMs": 9000000,
    "frameBatches": 5,
    "persistedFrames": 400,
    "rejectedFrames": 7,
    "eventCount": 3,
    "layoutId": "gt7_layout_1240",
    "detectedTrackId": "gt7_watkins_glen_international",
    "detectedLayoutId": "gt7_layout_1240"
  },
  "frameBatches": 5,
  "persistedFrames": 400,
  "timeRangeMs": {
    "from": 1000,
    "to": 80000
  },
  "lapsDetected": 6,
  "engineerEventCount": 3,
  "aiAuditLogCount": 1,
  "rejectionSummary": {
    "reasons": [
      {"code": "invalid_throttle", "count": 4},
      {"code": "invalid_speed", "count": 3}
    ]
  }
}
```

`timeRangeMs` is present only when at least one frame batch exists. In memory mode, `frameBatches` is `0`, `timeRangeMs` is derived from retained frames (if any), and `aiAuditLogCount` is always `0`.

`session.rejectedFrames` is always scoped to the requested session. `session.layoutId` is the effective session layout. `rejectionSummary` is detail-only and is omitted when no persisted rejection diagnostics exist. Reasons are merged across successful ingest requests and ordered by count descending, then code ascending. Admin stats intentionally do not expose rejection diagnostics.

## Planned V1 Endpoints
