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

- layout ID and name
- telemetry/generated path length in meters
- catalog length in meters from the selected seed layout
- delta meters and delta percent between telemetry path length and catalog length
- point count
- start/end gap
- deviation vs catalog centerline when a comparable baseline centerline exists

Example:

```text
trackbuilder closing report
layoutId: gt7_layout_1240
layoutName: Fuji Speedway
telemetry/generated path length meters: 1234.56m
catalogLengthMeters: 1240.00m
deltaMeters: -5.44m
deltaPct: -0.44%
points: 182
start-end gap: 0.83m
deviation vs catalog centerline: mean 1.12m max 4.57m
```

The key distinction is that `telemetry/generated path length meters` is the measured path produced from ingest, while `catalogLengthMeters` is the official seed-layout value. They are intentionally reported separately so length mismatch diagnostics are not confused with the catalog schema field.

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
