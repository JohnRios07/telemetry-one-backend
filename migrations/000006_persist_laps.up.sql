CREATE TABLE IF NOT EXISTS laps (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    lap_number INTEGER NOT NULL CHECK (lap_number >= 0),
    lap_time_ms BIGINT NOT NULL CHECK (lap_time_ms > 0),
    completed_at_unix_ms BIGINT NOT NULL CHECK (completed_at_unix_ms > 0),
    best_lap_ms BIGINT CHECK (best_lap_ms IS NULL OR best_lap_ms > 0),
    sample_count INTEGER CHECK (sample_count IS NULL OR sample_count > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (session_id, lap_number)
);

CREATE INDEX IF NOT EXISTS idx_laps_session_lap_number ON laps(session_id, lap_number);
CREATE INDEX IF NOT EXISTS idx_laps_session_completed_at ON laps(session_id, completed_at_unix_ms);
