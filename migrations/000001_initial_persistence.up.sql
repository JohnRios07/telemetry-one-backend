CREATE TABLE IF NOT EXISTS tracks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    layout_name TEXT NOT NULL DEFAULT '',
    country TEXT NOT NULL DEFAULT '',
    length_meters DOUBLE PRECISION NOT NULL CHECK (length_meters >= 0),
    fingerprint JSONB NOT NULL DEFAULT '{}'::jsonb,
    center_line JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS corners (
    id TEXT PRIMARY KEY,
    track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK (number >= 0),
    name TEXT NOT NULL,
    start_meters DOUBLE PRECISION NOT NULL CHECK (start_meters >= 0),
    apex_meters DOUBLE PRECISION NOT NULL CHECK (apex_meters >= 0),
    end_meters DOUBLE PRECISION NOT NULL CHECK (end_meters >= 0),
    direction TEXT NOT NULL DEFAULT '',
    radius_meters DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (radius_meters >= 0),
    severity TEXT NOT NULL DEFAULT '',
    auto_detected BOOLEAN NOT NULL DEFAULT false,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (confidence >= 0 AND confidence <= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT corners_distance_order CHECK (start_meters <= apex_meters AND apex_meters <= end_meters),
    CONSTRAINT corners_track_number_unique UNIQUE (track_id, number)
);

CREATE INDEX IF NOT EXISTS corners_track_id_idx ON corners(track_id);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    game TEXT NOT NULL,
    platform TEXT NOT NULL,
    driver_alias TEXT NOT NULL DEFAULT '',
    track_id TEXT REFERENCES tracks(id) ON DELETE SET NULL,
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sessions_time_order CHECK (ended_at IS NULL OR ended_at >= started_at)
);

CREATE INDEX IF NOT EXISTS sessions_track_id_idx ON sessions(track_id);
CREATE INDEX IF NOT EXISTS sessions_started_at_idx ON sessions(started_at);

CREATE TABLE IF NOT EXISTS engineer_events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    lap_number INTEGER NOT NULL CHECK (lap_number >= 0),
    corner_id TEXT REFERENCES corners(id) ON DELETE SET NULL,
    type TEXT NOT NULL,
    severity TEXT NOT NULL,
    timestamp_ms BIGINT NOT NULL CHECK (timestamp_ms > 0),
    metrics JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS engineer_events_session_id_idx ON engineer_events(session_id);
CREATE INDEX IF NOT EXISTS engineer_events_session_lap_idx ON engineer_events(session_id, lap_number);
CREATE INDEX IF NOT EXISTS engineer_events_corner_id_idx ON engineer_events(corner_id);
