# AI Consumer Contract

Phase 5.5 defines the backend-owned contract that future AI gateway code will consume. It is provider-neutral and does not include OpenRouter, model names, network calls, prompt templates, Flutter sync, or API-key ownership logic.

## Contract Version

AI input payloads use:

```text
telemetry-one.ai-consumer-input.v1
```

The Go type lives in `internal/ai.ConsumerInput`.

## Allowed Inputs

AI may receive only:

- Session context: session ID and nullable catalog references.
- Ordered Engineer events validated against `telemetry-one.engineer-event.v1`.
- Derived metric evidence already present inside Engineer events.
- Safety/redaction metadata that states the allowed input classes.
- Constraints that explain how unknown track/layout/corner states must be handled.

Allowed input kinds are closed:

| Kind | Meaning |
| --- | --- |
| `engineer_events` | Structured deterministic Engineer events. |
| `derived_metrics` | Derived metric evidence attached to events. |
| `catalog_refs` | Catalog-owned track/layout/corner references. |
| `session_context` | Minimal session context needed to explain events. |
| `unknown_states_explicit` | Unknown/no-corner states stay explicit; names are not invented. |

## Forbidden Inputs

AI must never receive:

- Raw `TelemetryFrame` objects.
- Frame batches such as `frames` or `telemetryFrames`.
- Position coordinates: `positionX`, `positionY`, `positionZ`.
- Raw control streams: `throttle`, `brake`, `steering`.
- Raw speed/drivetrain fields: `speedMps`, `rpm`, `gear`, wheel speeds.
- Raw yaw/fuel/on-track fields.

The decoder `ai.DecodeConsumerInputJSON` uses strict JSON decoding and maps raw telemetry-looking unknown fields to `ErrRawTelemetryField`. Event validation also rejects raw telemetry-looking metric names through the Engineer event contract.

## Catalog Names

Track, layout, and corner names may appear only through `events.CatalogRef` with `displayStrategy: "catalog_name"`, a non-empty catalog ID, and a catalog-owned display name.

If a catalog entity is unknown, the reference must be `null`. If only an ID is safe to expose, use `displayStrategy: "id_only"` and keep `name` null. GT7 UDP telemetry is never a source of official track, layout, sector, or corner names.

## Example Payload

```json
{
  "contractVersion": "telemetry-one.ai-consumer-input.v1",
  "session": {
    "sessionId": "session_01j2example",
    "track": {
      "id": "gt7_watkins_glen_international",
      "name": "Watkins Glen International",
      "displayStrategy": "catalog_name"
    },
    "layout": {
      "id": "gt7_watkins_glen_long_course",
      "name": "Long Course",
      "displayStrategy": "catalog_name"
    }
  },
  "events": [
    {
      "event": {
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
    }
  ],
  "safety": {
    "redactionPolicy": "no raw telemetry frames, coordinates, control streams, or frame batches",
    "allowedInputKinds": [
      "engineer_events",
      "derived_metrics",
      "catalog_refs",
      "session_context",
      "unknown_states_explicit"
    ]
  },
  "constraints": [
    "AI receives structured Engineer events and derived metric evidence only",
    "catalog display names may only come from Telemetry One catalog metadata",
    "unknown track, layout, or corner states must remain explicit"
  ]
}
```

## Future Gateway Boundary

Phase 7 can build an AI Gateway on top of this package. That gateway must remain backend-owned/proxied or use a user-owned OAuth/PKCE flow. Flutter must not ship app-owned provider API keys.
