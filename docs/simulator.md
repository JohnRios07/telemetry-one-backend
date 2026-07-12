# Synthetic Telemetry Simulator

Phase 1.5 adds a deterministic development fixture strategy for working without a PS5.

The simulator data is synthetic. It is not captured from Gran Turismo 7, is not official GT7 metadata, and must not be treated as a real track, layout, sector, or corner catalog. Track and corner names remain catalog-owned metadata for later phases.

## What It Produces

- A normalized `telemetry.IngestBatchRequest` matching `POST /api/v1/sessions/{sessionId}/frames`.
- One compressed synthetic lap around a fictional `Synthetic Dev Loop` shape.
- Deterministic speed, throttle, brake, steering, position, yaw, wheel-speed, fuel, and lap timing fields.
- Supported frame rates: `20`, `30`, and `60` Hz.

## Fixture File

The checked-in smoke fixture lives at:

```text
testdata/fixtures/synthetic_dev_loop_20hz.json
```

It is intentionally tiny so tests can load and validate fixture shape quickly. Use the generator for longer replay payloads.

## Generate A Replay Payload

```sh
go run ./cmd/simfixture -hz 20 -duration 9 -session synthetic-session-001
```

The command prints JSON to stdout. It does not call backend endpoints or persist frames.

The default duration is intentionally short so the generated payload stays below the current `600` frame batch limit at every supported frequency.

## Tradeoffs

- Deterministic math beats realism for this phase because contract validation and repeatable tests matter more than GT7 fidelity.
- The fixture avoids real track and corner names to keep GT7 UDP data separate from Telemetry One catalog metadata.
- The simulator does not implement production ingest, track detection, event generation, OpenRouter, or Flutter sync.
