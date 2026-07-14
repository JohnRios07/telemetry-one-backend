# Flutter Integration Contract

Phase 6.1 defines the stable contract between the current Flutter app and the Go backend. It does not implement Flutter sync, OpenRouter, prompt generation, provider networking, or full live streams.

The backend remains the source of truth for normalized ingest validation, catalog-backed track/layout names, corner-derived analysis, and structured Engineer events. Flutter remains the source of raw GT7 packet decoding and sends only normalized `TelemetryFrame` batches to the backend.

## Versioning And Scope

- HTTP base path: `/api/v1`.
- JSON field names are camelCase.
- Unknown JSON request fields are rejected by implemented endpoints.
- All timestamps crossing the HTTP boundary use Unix milliseconds unless a field explicitly says RFC3339.
- Current implemented endpoints are `POST /api/v1/sessions`, `POST /api/v1/sessions/{sessionId}/frames`, `GET /api/v1/sessions/{sessionId}/track`, `GET /api/v1/sessions/{sessionId}/events`, `POST /api/v1/sessions/{sessionId}/race-engineer/advice`, `GET /api/v1/sessions`, and `GET /api/v1/sessions/{sessionId}/summary`.
- `GET /api/v1/sessions/{sessionId}/live` and `GET /api/v1/sessions/{sessionId}/analysis` are planned contracts only in this phase.

## Error Envelope

All backend errors use the same envelope:

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

Flutter must branch on stable machine fields, not localized strings:

| Field | Rule |
| --- | --- |
| `error.code` | High-level HTTP/API class, for example `bad_request`, `not_implemented`, or `internal_error`. |
| `details.rejectionCode` | Stable ingest reason for retries and diagnostics. |
| `details.category` | `batch`, `frame`, or `consistency` for telemetry ingest. |
| `details.frameIndex` | Index inside the submitted batch when the rejection applies to one frame. |

## Create Session

```http
POST /api/v1/sessions
```

Status in this phase: request validation is implemented; durable session creation still returns `501 not_implemented` until runtime persistence is wired.

Request DTO:

```json
{
  "source": "flutter",
  "game": "gt7",
  "platform": "ps5",
  "driverAlias": "alex",
  "trackId": "gt7_watkins_glen_international",
  "startedUnixMs": 1720656000000
}
```

Fields:

| Field | Required | Rule |
| --- | --- | --- |
| `source` | Yes | Client source, normally `flutter`. |
| `game` | Yes | Game identifier, normally `gt7`. |
| `platform` | Yes | Platform identifier, normally `ps5`. |
| `driverAlias` | No | User-facing alias; backend does not require it. |
| `trackId` | No | Catalog ID from Telemetry One metadata or explicit user selection. Never from GT7 UDP names. |
| `startedUnixMs` | Yes | Session start in Unix milliseconds, greater than zero. |

Future success response DTO:

```json
{
  "session": {
    "id": "session_01j2example",
    "source": "flutter",
    "game": "gt7",
    "platform": "ps5",
    "driverAlias": "alex",
    "startedAt": "2026-07-11T00:00:00Z",
    "endedAt": null,
    "trackId": "gt7_watkins_glen_international"
  }
}
```

Flutter behavior:

- Treat `501 not_implemented` as an MVP backend limitation, not a user telemetry error.
- Once persistence exists, use the returned `session.id` for all frame, live, track, analysis, and event requests.
- Until then, development clients must call `POST /api/v1/sessions` to create an in-memory session and use the returned ID. V2 endpoints reject arbitrary local session IDs with `session_not_found`.

## Ingest Frame Batch

```http
POST /api/v1/sessions/{sessionId}/frames
```

Status in this phase: implemented.

Request DTO:

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
      "lapNumber": 2,
      "currentLapMs": 81234,
      "lastLapMs": 91345,
      "bestLapMs": 90210,
      "isOnTrack": true
    }
  ]
}
```

Optional backend frame fields may be sent only when Flutter has real source values:

```json
{
  "yawRadians": 1.57,
  "yawRate": 0.03,
  "wheelSpeedFL": 58.1,
  "wheelSpeedFR": 58.2,
  "wheelSpeedRL": 58.4,
  "wheelSpeedRR": 58.3
}
```

Flutter `TelemetryData` mapping:

| Flutter `TelemetryData` | Backend `TelemetryFrame` | Rule |
| --- | --- | --- |
| `timestamp` | `timestampUnixMs` | Convert `DateTime` to Unix milliseconds. |
| `speedKmh` | `speedMps` | Divide by `3.6`. |
| `rpm` | `rpm` | Preserve as RPM. |
| `gear` | `gear` | Preserve `-1` reverse, `0` neutral, `1+` forward. |
| `throttle` | `throttle` | Must be normalized `0.0..1.0`; do not clamp invalid data silently. |
| `brake` | `brake` | Must be normalized `0.0..1.0`; do not clamp invalid data silently. |
| `fuelCurrentL` | `fuelLiters` | Preserve liters. |
| `posX`, `posY`, `posZ` | `positionX`, `positionY`, `positionZ` | Preserve source coordinates. |
| `currentLap` | `lapNumber` | Preserve; `0` means unknown/not established. |
| `currentLapTime` | `currentLapMs` | Convert `Duration` to milliseconds; use `0` only when unknown. |
| `lastLapTime` | `lastLapMs` | Omit or send `null` when unknown. |
| `bestLapTime` | `bestLapMs` | Omit or send `null` when unknown. |
| none today | `yawRadians`, `yawRate` | Omit or send `null`; do not derive from positions in the ingest contract. |
| none today | `wheelSpeedFL/FR/RL/RR` | Omit or send `null`; do not synthesize from vehicle speed. |
| none today | `isOnTrack` | If Flutter cannot map a real source flag, send `false` only if source says false; otherwise add a real mapping before relying on it. |

Success response DTO (`202 Accepted`):

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

Batching and retry semantics:

- Send frames ordered by strictly increasing `timestampUnixMs` within each batch.
- Keep batches at or below `600` frames.
- Ingest is all-or-nothing: a bad frame rejects the whole batch and retains none of it.
- Retry transient network failures with the same ordered batch and same `sessionId`.
- Do not blindly retry deterministic `bad_request` rejections. Use `details.rejectionCode` and `details.frameIndex` to drop, fix, or quarantine the bad frame before resubmitting a new valid batch.
- A `session_id_mismatch` is a client bug: route `{sessionId}` and body `sessionId` must match, or Flutter can omit the body `sessionId` and rely on the route.
- A `session_not_found` error means the session does not exist or was lost (e.g. server restart). Do not retry with the same session ID. Instead, create a new session via `POST /api/v1/sessions`, update `BackendSyncState.sessionId`, and flush buffered frames under the new ID.

## Live State And Track Detection

### Implemented Track Detection

```http
GET /api/v1/sessions/{sessionId}/track
```

Response DTO:

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

Fallback response DTO:

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

Flutter behavior:

- `pending`: keep ingesting frames and keep track-dependent UI disabled or loading.
- `detected`: display catalog-owned `trackName`/`layoutName` and use IDs for downstream analysis.
- `low_confidence`, `ambiguous`, `unknown`: prompt for manual selection or keep unknown; do not auto-invent official names.

### Planned Live Snapshot

```http
GET /api/v1/sessions/{sessionId}/live
```

Status in this phase: planned/stub contract. Not implemented yet.

Planned response DTO:

```json
{
  "sessionId": "session_01j2example",
  "status": "active",
  "lastFrameUnixMs": 1720656000123,
  "trackDetection": {
    "status": "detected",
    "trackId": "gt7_watkins_glen_international",
    "layoutId": "gt7_watkins_glen_long_course",
    "trackName": "Watkins Glen International",
    "layoutName": "Watkins Glen Long Course",
    "confidence": 0.85,
    "reasons": ["length_match"],
    "nextAction": "use_detected_catalog_layout"
  },
  "lap": {
    "lapNumber": 2,
    "currentLapMs": 81234,
    "lastLapMs": 91345,
    "bestLapMs": 90210
  },
  "position": {
    "distanceFromStartMeters": 732.4,
    "progress": 0.135,
    "cornerId": "watkins_glen_long_turn_1",
    "cornerName": "Turn 1"
  },
  "retention": {
    "retainedFrames": 1200,
    "retainedFramesLimit": 12000
  }
}
```

Planned live rules:

- `cornerName` is nullable and may appear only from catalog metadata.
- `distanceFromStartMeters`, `progress`, and corner fields are nullable until distance/corner processing is attached to the API.
- The live endpoint should expose derived state only. It must not echo raw frame coordinates, throttle, brake, speed, or wheel streams.

## Analysis And Events

### Planned Analysis Snapshot

```http
GET /api/v1/sessions/{sessionId}/analysis
```

Status in this phase: planned/stub contract. Not implemented yet.

Query parameters planned for first implementation:

| Parameter | Required | Rule |
| --- | --- | --- |
| `lapNumber` | No | Filter by lap, zero or greater. |
| `cornerId` | No | Filter by catalog corner ID. |

Planned response DTO:

```json
{
  "sessionId": "session_01j2example",
  "status": "partial",
  "track": {
    "id": "gt7_watkins_glen_international",
    "name": "Watkins Glen International",
    "displayStrategy": "catalog_name"
  },
  "layout": {
    "id": "gt7_watkins_glen_long_course",
    "name": "Watkins Glen Long Course",
    "displayStrategy": "catalog_name"
  },
  "laps": [
    {
      "lapNumber": 2,
      "corners": [
        {
          "status": "complete",
          "cornerId": "watkins_glen_long_turn_1",
          "cornerName": "Turn 1",
          "number": 1,
          "sampleCount": 42,
          "entrySpeedKph": {"status": "available", "value": 231.2},
          "minimumSpeedKph": {"status": "available", "value": 111.8},
          "exitSpeedKph": {"status": "available", "value": 158.5},
          "maxBrakePercent": {"status": "available", "value": 82},
          "firstThrottleReapplication": {
            "status": "available",
            "distanceFromStart": 820.4,
            "timestampUnixMs": 1720656012345,
            "timeOffsetMs": 320
          },
          "exitAccelerationDeltaKph": {"status": "available", "value": 46.7}
        }
      ]
    }
  ]
}
```

Planned analysis rules:

- Analysis responses contain derived corner metrics only, never raw frame streams.
- Metric objects use `status: "available"` or `status: "unavailable"`; unavailable metrics include a stable `reason` when known.
- Track/layout/corner names remain catalog-owned and nullable.

### Implemented Engineer Events

```http
GET /api/v1/sessions/{sessionId}/events
```

Status in this phase: implemented.

Query parameters:

| Parameter | Required | Rule |
| --- | --- | --- |
| `lapNumber` | No | Return events for one lap. Must be zero or greater. |
| `cornerId` | No | Return events whose catalog corner ref ID matches. |
| `type` | No | One current supported type: `early_braking`, `late_braking`, `late_throttle`, `low_exit_speed`, or `inconsistent_corner`. |

Response DTO:

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
        "name": "Watkins Glen Long Course",
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

Flutter behavior:

- Render events as deterministic facts, not AI prose.
- Treat empty `events: []` as a valid state.
- Do not expect raw telemetry fields in event payloads. Fields such as `positionX`, `speedMps`, `throttle`, `brake`, and wheel speeds are intentionally forbidden.
- Respect `displayStrategy`: `catalog_name` can display `name`, `id_only` can display or debug with `id` only, and `null` means unknown.

### Implemented Live Race Engineer Advice

```http
POST /api/v1/sessions/{sessionId}/race-engineer/advice
```

Status in this phase: implemented in the backend only. Flutter UI integration is intentionally deferred.

Request DTO (optional body):

```json
{
  "sinceUnixMs": 1720656000000,
  "maxEvents": 5
}
```

Flutter MUST send only optional window fields:

| Field | Required | Rule |
| --- | --- | --- |
| `sinceUnixMs` | No | Unix milliseconds lower bound for stored engineer events. Must be greater than zero when present. |
| `maxEvents` | No | Defaults to `5`; backend clamps to `1..10`. |

Flutter MUST NOT send provider API keys, provider names, model names, prompts, raw telemetry frames, `ConsumerInput`, or AI configuration. The backend validates the session, reads stored `engineer_events`, builds AI input server-side, and calls the configured backend AI pipeline. The default provider remains `fake` unless backend environment configuration selects OpenRouter.

Response DTO:

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

Client behavior:

- `status: "no_events"` is a normal 200 response. Render the deterministic message or wait for more events; do not treat it as a transport error.
- `referencedEvents` identifies the exact stored events used for advice and can be used to avoid duplicate display across polling windows.
- `window.providerCalled` is `false` for `no_events`; the backend intentionally skips AI provider invocation in that state.
- Gateway fallback statuses (`provider_error`, `budget_limited`, `rate_limited`, `invalid_response`) may be returned as 200 responses with safe deterministic text from the backend.
- Do not store or infer provider secrets on the client. Provider info is observability metadata only.

## Session History And Summary

### List Sessions

```http
GET /api/v1/sessions?limit=20
```

Status in this phase: implemented.

Returns recent sessions with aggregate frame, batch, and event counts. No auth required.

| Query parameter | Default | Range | Description |
|---|---|---|---|
| `limit` | `20` | `1..100` | Number of recent sessions. Out-of-range values are silently clamped. |

Response DTO:

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
      "eventCount": 3,
      "detectedTrackId": null,
      "detectedLayoutId": null
    }
  ]
}
```

In memory mode, `frameBatches` is always `0`. `detectedTrackId` and `detectedLayoutId` are reserved and always omitted in the current implementation.

### Session Summary

```http
GET /api/v1/sessions/{sessionId}/summary
```

Status in this phase: implemented.

Returns aggregate counts for a single session. Session must exist; returns `404 session_not_found` otherwise.

Response DTO:

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
    "eventCount": 3
  },
  "frameBatches": 5,
  "persistedFrames": 400,
  "timeRangeMs": {
    "from": 1000,
    "to": 80000
  },
  "lapsDetected": 6,
  "engineerEventCount": 3,
  "aiAuditLogCount": 1
}
```

`timeRangeMs` is present only when at least one frame batch exists. In memory mode, `frameBatches` is `0`, `timeRangeMs` is derived from retained frames (if any), and `aiAuditLogCount` is always `0`.

Rejection stats are not included — rejection summaries are not persisted on the backend.

## Flutter Sync Resilience (Phase 6.3)

`BackendSyncNotifier` exposes a `SyncStatus` enum that drives client-side resilience:

| Status | Meaning | UI hint |
| --- | --- | --- |
| `disabled` | Sync is turned off by the user or `setEnabled(false)`. | No sync UI or disabled toggle shown. |
| `idle` | Sync is enabled, no errors, backend reachable. | Normal operation. |
| `syncing` | Actively flushing a frame batch. | Activity indicator. |
| `degraded` | Network/server errors detected; frames buffered locally and will retry. | "Upload paused — retrying" banner. |
| `rejected` | Backend returned a typed validation rejection (4xx with `details`). The batch was dropped, NOT retried. | "Data validation error" with rejection details. |
| `failed` | Buffer overflow occurred; oldest frames were dropped to prevent unbounded memory growth. | "Some data lost" warning. |

Error classification in `_flush()`:

- **Typed rejection (4xx `bad_request` with `error.details` representing an `IngestRejection`)**: Frame batch is dropped. Status → `rejected`. `totalRejected` incremented by batch size. `lastRejection` stores the rejection details. `consecutiveFailures` resets to 0.
- **Network error (SocketException, HttpException)**: Frames re-buffered at front of queue. Status → `degraded`. `consecutiveFailures` incremented. `isOffline` getter returns `true`.
- **Server error (5xx `internal_error`, `service_unavailable`)**: Same as network error — frames buffered, `consecutiveFailures` incremented.
- **Buffer overflow**: When buffered frames exceed `maxBatchSize * 2`, oldest frames are dropped. Status → `failed` with a message indicating how many frames were lost.
- **Recovery**: A successful flush from any error state resets `status` → `idle`, clears `lastErrorMessage`, `lastRejection`, and resets `consecutiveFailures` to 0.

Key properties:

- `enabled`: `true` when `status != SyncStatus.disabled`.
- `isOffline`: `true` when `status` is `degraded` or `failed`.
- `pendingFrames`: number of frames currently in the local buffer waiting to be sent.
- `consecutiveFailures`: count of sequential flush failures (resets on success).
- `lastRejection`: the typed `IngestRejection` from the backend, if the last error was a validation rejection.
- `lastErrorMessage`: human-readable last error message.
- `lastErrorAt` / `lastSyncAt`: timestamps for diagnostics.

Local recording/analytics are unaffected by backend sync status. The sync layer is purely additive — failures never interfere with local V1 functionality.

## Gradual V2 Data Bridge (Phase 6.4)

The V2 data bridge provides an opt-in abstraction layer for backend-sourced data without breaking local V1 recording or analytics.

### Architecture

```
Feature flag (useV2Data: false by default)
  │
  ├── false → BackendDataBridge returns null immediately — no network calls
  │            V1 local providers remain the exclusive source of truth
  │
  └── true  → Bridge calls implemented backend endpoints:
               ├── GET /sessions/{id}/track     → TrackDetectionResponse?
               └── GET /sessions/{id}/events     → List<EngineerEvent>?
               (live & analysis remain planned/stub — NOT wrapped)
               On any error (network, 5xx, 501) → returns null
```

### Components

| File | Role |
| --- | --- |
| `lib/core/backend/backend_config.dart` | `useV2Data` feature flag (default: `false`) |
| `lib/core/backend/backend_data_bridge.dart` | Read-only bridge that wraps backend calls with flag check + error → null |
| `lib/core/backend/v2_bridge_providers.dart` | Riverpod providers: `backendTrackDetectionProvider`, `backendEventsProvider`, `backendV2EnabledProvider` |

### Migration Strategy (Feature by Feature)

| Feature | Backend Status | Bridge | V1 Fallback |
| --- | --- | --- | --- |
| Frame sync | Implemented | BackendSyncNotifier (Phases 6.2–6.3) | Frames buffered locally on network error |
| Track detection | Implemented | `BackendDataBridge.getTrackDetection` → `backendTrackDetectionProvider` | Returns null → V1 local session data used |
| Engineer events | Implemented | `BackendDataBridge.getEvents` → `backendEventsProvider` | Returns null → V1 local analytics used |
| Live snapshot | Planned/stub | Not wrapped — returns null | TelemetryStreamProvider (V1 live) |
| Analysis snapshot | Planned/stub | Not wrapped — returns null | SessionAnalyzer (V1 local) |

### Rules

- The bridge never writes state. It only returns data or null.
- Null from the bridge means "V1 is the source of truth" — UI continues using local providers.
- Planned endpoints (`live`, `analysis`) are NEVER called through the bridge.
- Track/corner names come exclusively from the backend catalog via `TrackDetectionResponse`. The bridge never invents official names.
- Feature flag default is `false`. Enable by setting `BackendConfig(useV2Data: true)`.
- Session ID is read from `BackendSyncState.sessionId` (returned by `POST /api/v1/sessions`). V2 endpoints reject arbitrary local IDs.

### Tests

`test/backend/backend_data_bridge_test.dart` covers:
- V2 disabled → no backend calls, null results (V1 unaffected)
- V2 enabled → track detection and events fetched
- V2 enabled + 501 → null (graceful)
- V2 enabled + network error → null (graceful)
- V2 enabled + server error → null (graceful)
- No fake track/corner names
- Planned endpoints never called

## Engineer + Coach Compatibility Strategy (Phase 6.5)

V2 backend data is layered ON TOP of V1 local data — it never replaces it.

### Architectural Guarantee

V1 Engineer/Coach providers have **zero import dependencies** on backend code. The proof is the import graph:

```
engineer_session_providers.dart imports:
  ├── core/storage/session_model.dart
  ├── core/storage/session_repository.dart
  ├── features/engineer/analytics/*.dart
  └── features/engineer/domain/*.dart
  └── (NO backend_* imports)
```

This means V1 local providers (`engineerSessionProvider`, `engineerSessionSummaryProvider`, `engineerLapComparisonProvider`, `engineerRecommendationsProvider`, `engineerCoachReportProvider`) are architecturally isolated from V2 bridge code. A compile error would occur if a V1 provider accidentally depended on backend types.

### V2 Augmentation Provider

`lib/core/backend/v2_engineer_compatibility.dart` exposes `engineerV2AugmentationProvider`:

```dart
class EngineerV2Augmentation {
  final bool v2Enabled;
  final TrackDetectionResponse? trackDetection;
  final String status; // 'v2_disabled' | 'no_data' | 'track_detected'
  bool get hasBackendData => trackDetection != null;
}
```

This provider:
- Returns `v2_disabled` by default (when `useV2Data` is `false`).
- Returns `no_data` when V2 is enabled but backend is unreachable, returns 501, or track is pending.
- Returns `track_detected` when backend track detection is available.
- Never throws. On any error, returns a valid `EngineerV2Augmentation` with `status: 'no_data'`.

### UI Wiring Deferred

The Engineer detail screen (`engineer_session_detail_screen.dart`) is NOT modified in this phase. Rationale:

1. **Session ID mismatch**: The V2 bridge tracks the backend sync sessionId (`local_*`), while V1 Engineer reads sessions from local Hive storage with their own IDs. These IDs don't correspond until create-session persistence is implemented (currently 501).

2. **Minimal disruption**: The Engineer detail screen is stable with 1024 lines of reviewed UI code. Adding a conditional V2 section before session alignment exists would add speculative complexity.

3. **V2 bridge is opt-in**: Even with `useV2Data: true`, backend data is null until a backend session exists with detected track data. A UI section that is always hidden/empty would confuse users.

### Test Coverage

| Test File | Tests | What It Proves |
| --- | --- | --- |
| `test/backend/v2_engineer_compatibility_test.dart` | 17 | V1 providers independent, V2 bridge null-safe, catalog names only, no fake corners, stateless bridge |
| `test/backend/backend_data_bridge_test.dart` | 20 | V2 flag gating, error → null, planned endpoints never called |
| `test/coach_analyzer_test.dart` | 2 | Existing coach output unchanged |

### Compatibility Checklist

- [x] Default V1 Engineer/Coach outputs unchanged when V2 disabled
- [x] Enabling V2 bridge with null backend data does not break V1
- [x] Enabling V2 with backend events/track can be surfaced without replacing local report
- [x] No fake track/corner names — all names sourced from catalog via backend
- [x] Existing coach analyzer tests still pass (verified: 2/2)
- [x] V2 bridge is stateless — never writes state
- [x] Planned endpoints (live, analysis) are NOT wrapped by bridge
- [x] UI wiring deferred until session ID alignment is resolved (create-session persistence)

## Contract Decisions And Tradeoffs

- HTTP polling stays the Phase 6.1 contract. WebSocket/live streaming can be added later without changing these DTOs.
- Missing Flutter source fields are omitted/null instead of synthesized. This protects deterministic engines from fake yaw, wheel speed, or on-track data.
- Ingest allows raw normalized frames only at the backend ingest boundary. Analysis, event, and future AI contracts expose derived state/events only.
- Planned live/analysis DTOs are documented but not implemented as Go structs yet. That keeps this slice stable without committing runtime APIs before the backend has repositories for those snapshots.
- Catalog display names are backend/catalog-owned. GT7 UDP data may help geometry and detection, but never official names.
