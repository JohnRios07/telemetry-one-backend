# GT7Tracks Fixture Samples

These fixtures are development/test data only. They are **not** production track geometry, sector definitions, apex positions, corner names, or centerline truth.

## Attribution

- Source project: [`vthinsel/GT7Tracks`](https://github.com/vthinsel/GT7Tracks)
- Upstream repository README confirms raw dump CSV columns are captured car position `x/z/y` plus `speed/rpm` and optional native telemetry.
- Confirmed raw dump schema from upstream `dumps/*.csv` on 2026-07-16: `track_id,x,z,y,speed,rpm,orientation,rotation_x,rotation_z,rotation_y`.

## Fixture Boundary

- JSON files in this directory use only `telemetry.IngestBatchRequest` shape (`sessionId` + `frames`).
- Raw dump-derived or minimal CSV data may be used to regenerate test fixtures with `go run ./cmd/gt7convert`, but must stay under `testdata` or local developer scratch space.
- Do not wire these fixtures into production catalog, geometry, current-corner resolver, centerline, sector, or apex flows.
- Do not invent GT7 corner names from these samples. Length-only detection remains conservative and must allow `ambiguous`/`unknown` fallback.
