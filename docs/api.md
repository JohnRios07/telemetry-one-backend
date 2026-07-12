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
  "status": "ok",
  "env": "development",
  "time": "2026-07-11T00:00:00Z"
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

`trackId` is optional because GT7 UDP does not provide authoritative track names. When present, it must come from Telemetry One catalog metadata or explicit user selection, not from raw UDP fields.

## Telemetry Frames

```http
POST /api/v1/sessions/{sessionId}/frames
```

Accepts normalized frame batches sent by Flutter. The current implementation validates and normalizes the whole batch, stores accepted frames in a bounded in-memory per-session buffer, then returns an acknowledgement without DB-backed frame persistence. Distance/corner processing is deferred to later phases.

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
- The batch is accepted all-or-nothing. Any invalid frame rejects the request with the standard error envelope.
- Frame validation errors include the failing frame index, for example `frames[0]: throttle must be between 0 and 1`.

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

Success response (`202 Accepted`):

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

The accepted time range is calculated from the accepted batch timestamps. `rejectedFrames` is `0` on success because ingest uses whole-batch rejection instead of partial acceptance.

See `docs/ingest-performance.md` for expected frame rates, batch-size coverage, retention windows, current ingest benchmark coverage, and MVP limits.

## Engineer Events

```http
GET /api/v1/sessions/{sessionId}/events
```

Returns structured Engineer events already accepted by the backend for a session. The endpoint does not generate events from frames; it exposes events that were created by deterministic code paths, validated against the Engineer event contract, deduplicated, and stored in the MVP in-memory event repository.

Engineer events are deterministic facts derived from validated analysis metrics and context. They are not free-form AI messages and they never include raw telemetry frames.

Phase 5.2 adds a pure deterministic rule engine in `internal/events`. It consumes per-corner analysis metrics, optional reference metrics, catalog references, lap/session context, and configurable rule thresholds.

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

## Detect Session Track

```http
GET /api/v1/sessions/{sessionId}/track
```

Runs the deterministic track detector over retained frames for the session and the official GT7 seed catalog. The detector is conservative: it only uses observed completed lap length, requires enough retained frames, and returns explicit fallback states when evidence is insufficient, weak, absent from the catalog, or ambiguous.

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

## Planned V1 Endpoints

These endpoints are documented from the reference spec but are not implemented in the scaffold slice yet.

```http
GET /api/v1/sessions/{sessionId}/live
GET /api/v1/sessions/{sessionId}/analysis
GET /api/v1/sessions/{sessionId}/laps/{lapNumber}
GET /api/v1/sessions/{sessionId}/laps/{lapNumber}/corners
POST /api/v1/tracks
```

See `docs/flutter-contract.md` for the Phase 6.1 backend/Flutter contract for implemented endpoints and the planned live/analysis response shapes.
