# Migrations

Phase 1.4 introduced the initial PostgreSQL schema for durable catalog, session, corner, and structured event data. The V2 sessions persistence milestone adds an embedded runtime migration runner that applies `*.up.sql` files in filename order when `TELEMETRY_ONE_DATABASE_URL` is configured.

The initial schema stores:

- `tracks`: Telemetry One catalog metadata, including fingerprint and centerline JSONB placeholders for later phases.
- `corners`: catalog-owned corner metadata; track and corner names are not sourced from GT7 UDP.
- `sessions`: session lifecycle and optional selected/detected `track_id`.
- `engineer_events`: structured Engineer events and derived metrics only, never raw telemetry frames.

`000002_sessions_lifecycle.up.sql` is intentionally idempotent around the `sessions` table/indexes so environments that already applied `000001_initial_persistence.up.sql` can move forward without schema drift.
