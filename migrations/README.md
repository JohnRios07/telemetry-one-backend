# Migrations

Phase 1.4 introduces the initial PostgreSQL schema for durable catalog, session, corner, and structured event data.

Apply migrations in filename order with the project migration runner once runtime database wiring is added. This slice intentionally keeps migrations as plain SQL and does not add a Go database driver yet.

The initial schema stores:

- `tracks`: Telemetry One catalog metadata, including fingerprint and centerline JSONB placeholders for later phases.
- `corners`: catalog-owned corner metadata; track and corner names are not sourced from GT7 UDP.
- `sessions`: session lifecycle and optional selected/detected `track_id`.
- `engineer_events`: structured Engineer events and derived metrics only, never raw telemetry frames.
