# Telemetry Frame Normalization

Phase 2.1 defines the canonical backend `TelemetryFrame` contract. Phase 2.2 uses these same rules in the HTTP batch ingest endpoint, and Phase 2.3 adds typed rejection details for invalid or inconsistent batches. It does not implement ingest persistence, track detection, event generation, or AI payloads.

## Canonical Units

- `timestampUnixMs`: Unix timestamp in milliseconds, greater than zero.
- `speedMps`: vehicle speed in meters per second, finite and zero or greater.
- `rpm`: engine speed in RPM, finite and zero or greater.
- `throttle` and `brake`: normalized pedal inputs from `0.0` to `1.0`.
- `steering`: normalized steering input from the source model when available; must be finite. The backend does not clamp or reinterpret the range in Phase 2.1.
- `fuelLiters`: fuel amount in liters, finite and zero or greater.
- `positionX`, `positionY`, `positionZ`: world position in source coordinates, finite. Distance engines consume these as meters later.
- `yawRadians` and `yawRate`: radians/radians per second when provided. Missing values stay unknown.
- `wheelSpeedFL`, `wheelSpeedFR`, `wheelSpeedRL`, `wheelSpeedRR`: wheel linear speeds in meters per second when provided. Missing values stay unknown.
- `lapNumber`: zero or greater. `0` means unknown/not established yet.
- `currentLapMs`, `lastLapMs`, `bestLapMs`: lap times in milliseconds. Historical lap times are optional because Flutter may not have them yet.
- `isOnTrack`: source-provided track state when known. Phase 2.1 does not infer it from coordinates.

## Validation Behavior

- Invalid numeric values are rejected, not clamped.
- `NaN` and `Inf` are rejected for every numeric field.
- Negative speed, RPM, fuel, wheel speeds, lap numbers, and lap times are rejected.
- `throttle` and `brake` outside `0.0..1.0` are rejected.
- Missing optional fields are preserved as `nil`/omitted JSON, not converted to `0`.
- Yaw angles are preserved as supplied; Phase 2.1 does not wrap them into a fixed range to avoid introducing discontinuities before distance/corner processing exists.
- Validation failures are exposed as stable rejection codes and categories in the API error `details` object. User-facing clients should key retry and diagnostics from `details.rejectionCode`, not from the human-readable message.

## Batch Ingest Behavior

- `POST /api/v1/sessions/{sessionId}/frames` accepts up to 600 frames per batch.
- `sessionId` may be supplied in the body or inferred from the route, but when both are present they must match.
- Batch ingest is all-or-nothing in Phase 2.2. The backend returns `202 Accepted` only when every frame validates.
- Phase 2.3 rejects intra-batch inconsistencies: non-increasing timestamps, lap number regressions, and `currentLapMs` regressions within the same lap. `currentLapMs` may reset when `lapNumber` increases.
- Invalid batches return the standard API error envelope with `bad_request` plus typed rejection details when the failure comes from telemetry validation. Invalid frame and consistency errors include the failing frame index.
- Successful responses are acknowledgements only. Frames are not persisted or buffered yet; Phase 2.4 owns retention/storage policy.

## Flutter Mapping Notes

Flutter `TelemetryData` currently maps to the backend frame as follows:

| Flutter `TelemetryData` | Backend `Frame` | Rule |
| --- | --- | --- |
| `timestamp` | `timestampUnixMs` | Convert `DateTime` to Unix milliseconds. |
| `speedKmh` | `speedMps` | Divide by `3.6`. |
| `rpm` | `rpm` | Preserve as RPM. |
| `gear` | `gear` | Preserve `-1` reverse, `0` neutral, `1+` forward. |
| `throttle` | `throttle` | Must already be `0.0..1.0`. |
| `brake` | `brake` | Must already be `0.0..1.0`. |
| `fuelCurrentL` | `fuelLiters` | Preserve liters. |
| `posX`, `posY`, `posZ` | `positionX`, `positionY`, `positionZ` | Preserve source coordinates. |
| `currentLap` | `lapNumber` | Preserve; `0` means unknown/not established. |
| `currentLapTime` | `currentLapMs` | Convert `Duration` to milliseconds; use `0` only when unknown. |
| `lastLapTime` | `lastLapMs` | Omit/null when unknown. |
| `bestLapTime` | `bestLapMs` | Omit/null when unknown. |

Flutter's shared `TelemetryData` does not currently expose yaw or wheel speeds. Those backend fields are optional and must be omitted/null until Flutter maps real protocol values. Do not synthesize them from position deltas in the ingest contract.

Track names, layout names, sectors, and corner names are never populated from GT7 UDP. They come only from Telemetry One catalog metadata or explicit user selection in later phases.
