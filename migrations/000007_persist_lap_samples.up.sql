CREATE TABLE IF NOT EXISTS lap_samples (
    id TEXT PRIMARY KEY,
    lap_id TEXT NOT NULL REFERENCES laps(id) ON DELETE CASCADE,
    distance_meters INTEGER NOT NULL CHECK (distance_meters >= 0 AND distance_meters % 5 = 0),
    source TEXT NOT NULL,
    timestamp_unix_ms BIGINT,
    speed_mps DOUBLE PRECISION,
    rpm DOUBLE PRECISION,
    gear INTEGER,
    throttle DOUBLE PRECISION,
    brake DOUBLE PRECISION,
    steering DOUBLE PRECISION,
    fuel_liters DOUBLE PRECISION,
    yaw_radians DOUBLE PRECISION,
    yaw_rate DOUBLE PRECISION,
    wheel_speed_fl DOUBLE PRECISION,
    wheel_speed_fr DOUBLE PRECISION,
    wheel_speed_rl DOUBLE PRECISION,
    wheel_speed_rr DOUBLE PRECISION,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (lap_id, distance_meters)
);

CREATE INDEX IF NOT EXISTS idx_lap_samples_lap_distance ON lap_samples(lap_id, distance_meters);
