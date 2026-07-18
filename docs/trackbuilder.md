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
- source point count after invalid-frame filtering
- source segment count after filtering duplicate coordinates
- source segment length summary (min/p50/p95/max)
- source planar heading change in X/Z total and per meter
- source local chord excess on 4-point windows: evaluated window count, path/chord ratio, and excess meters, summarized as avg/p50/p95/max
- source path length in meters before simplification and resampling
- simplified point count after RDP simplification
- simplification dropped points versus the cleaned source
- generated point count in the final centerline
- generated path length in meters after the pipeline output
- catalog length in meters from the selected seed layout
- delta meters and delta percent between telemetry path length and catalog length
- start/end gap
- deviation vs catalog centerline when a comparable baseline centerline exists

Example:

```text
trackbuilder closing report
layoutId: gt7_layout_1240
layoutName: Fuji Speedway
source point count: 182
source segment count: 181
source segment length meters: min 0.83m p50 6.41m p95 18.52m max 42.10m
source planar heading change (X/Z): 1428.44deg total, 1.1570deg/m
source local chord excess (4-point windows, 2 windows): ratio avg 1.0184 p50 1.0102 p95 1.0571 max 1.1243; excess meters avg 0.42m p50 0.18m p95 1.07m max 2.18m
source path length meters: 1234.56m
simplified point count: 96
simplification dropped points: 86
generated point count: 96
generated path length meters: 1234.56m
catalogLengthMeters: 1240.00m
deltaMeters: -5.44m
deltaPct: -0.44%
start-end gap: 0.83m
deviation vs catalog centerline: mean 1.12m max 4.57m
```

The key distinction is that `source path length meters` measures the usable telemetry polyline before simplification/resampling, while `generated path length meters` measures the final centerline written into the curated catalog. `catalogLengthMeters` remains the official seed-layout value. They are intentionally reported separately so length mismatch diagnostics are not confused with the catalog schema field.

The new source roughness metrics are report-only diagnostics. They are meant to surface telemetry zigzags and short noisy segments without changing the generated catalog geometry. Heading change and local chord excess are measured in the plan view (X/Z) so vertical-only motion does not inflate the diagnostics. The local chord ratio is a heuristic: higher values can mean tight curvature or noisy micro-zigzags, but it does not prove bad telemetry on its own.

## Promotion Caveat

This output is review-only. It is not runtime truth and must not be auto-promoted into the production catalog.

## Exporting Session Frames

Use `export-session-frames` to turn a persisted backend session into the exact `telemetry.IngestBatchRequest` JSON that `trackbuilder` consumes.

```bash
export-session-frames -session-id session_01KXSTFTPV5G3KT61TTSVGR405 -lap-number 3 -output ingest.json
trackbuilder -input ingest.json -layout-id gt7_layout_1240 -output catalog.json
```

Flags:

- `-session-id`: required session ID to export
- `-lap-number`: optional lap number to export; omit to export all frames
- `-output`: required JSON output path
- `-database-url`: optional Postgres URL; falls back to `TELEMETRY_ONE_DATABASE_URL`
- `-allow-active-session`: opt in to exporting an unfinished session snapshot

The exporter is read-only, preserves persisted frame order, and fails closed for active sessions unless the explicit opt-in flag is present.

If active-session export is enabled, the CLI prints a warning to `stderr` because the snapshot may be incomplete.

## HTTP Export

The backend also exposes `GET /api/v1/sessions/{sessionId}/export/trackbuilder` for controlled export workflows.

- The endpoint is disabled by default and only works when `TELEMETRY_ONE_ENABLE_SESSION_EXPORT=true`.
- Success returns the raw `telemetry.IngestBatchRequest` JSON body with `Content-Type: application/json`.
- The response is not wrapped in the normal API envelope, so treat it as raw telemetry.
- Add `?lapNumber=3` to export a single complete lap batch, for example `session_01KXSTFTPV5G3KT61TTSVGR405`.
- Active sessions are rejected; do not use this endpoint as an ad hoc ingestion or debugging shortcut.
