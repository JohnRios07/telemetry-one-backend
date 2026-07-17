ALTER TABLE laps
ADD COLUMN IF NOT EXISTS telemetry_gap_count INTEGER NOT NULL DEFAULT 0 CHECK (telemetry_gap_count >= 0);
