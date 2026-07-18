# Trackbuilder

`trackbuilder` converts ordered `telemetry.IngestBatchRequest` JSON into a single-layout curated track catalog for review only.

## Usage

```bash
trackbuilder -input ingest.json -layout-id gt7_layout_1240 -output catalog.json
```

Flags:

- `-input`: ingest request JSON file
- `-layout-id`: seed layout to curate
- `-output`: optional catalog output path; defaults to stdout
- `-smoothing-window`: moving average window, default `3`
- `-epsilon`: RDP simplification epsilon in meters, default `0.5`

## Output

- Catalog JSON is schema-shaped and contains one track and one layout.
- `sectors` and `corners` stay empty.
- `centerLine` is generated from the telemetry input and includes `accumulatedMeters`.
- `sourceType` is marked `telemetry_one_curated_geometry` to make the artifact clearly non-official.

## Report

The CLI writes a separate human-readable closing report to `stderr` with:

- length
- point count
- start/end gap
- deviation vs catalog when a comparable baseline centerline exists

## Promotion Caveat

This output is review-only. It is not runtime truth and must not be auto-promoted into the production catalog.

## Exporting Session Frames

Use `export-session-frames` to turn a persisted backend session into the exact `telemetry.IngestBatchRequest` JSON that `trackbuilder` consumes.

```bash
export-session-frames -session-id session-123 -output ingest.json
trackbuilder -input ingest.json -layout-id gt7_layout_1240 -output catalog.json
```

Flags:

- `-session-id`: required session ID to export
- `-output`: required JSON output path
- `-database-url`: optional Postgres URL; falls back to `TELEMETRY_ONE_DATABASE_URL`
- `-allow-active-session`: opt in to exporting an unfinished session snapshot

The exporter is read-only, preserves persisted frame order, and fails closed for active sessions unless the explicit opt-in flag is present.

If active-session export is enabled, the CLI prints a warning to `stderr` because the snapshot may be incomplete.
