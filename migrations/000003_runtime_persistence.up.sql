CREATE TABLE IF NOT EXISTS frame_batches (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    lap_number INTEGER NOT NULL CHECK (lap_number >= 0),
    batch_index INTEGER NOT NULL CHECK (batch_index >= 0),
    frame_count INTEGER NOT NULL CHECK (frame_count > 0),
    frames_jsonb JSONB NOT NULL,
    time_start_ms BIGINT NOT NULL,
    time_end_ms BIGINT NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT frame_batches_time_order CHECK (time_start_ms <= time_end_ms)
);

CREATE INDEX IF NOT EXISTS frame_batches_session_id_idx ON frame_batches(session_id);
CREATE INDEX IF NOT EXISTS frame_batches_session_lap_idx ON frame_batches(session_id, lap_number);
CREATE UNIQUE INDEX IF NOT EXISTS frame_batches_session_order_idx ON frame_batches(session_id, batch_index);

-- Runtime ingest must continue accepting Flutter/local session IDs until the
-- Flutter backend-session mapping slice lands. Existing deployments created
-- engineer_events with a sessions FK, so relax it without deleting data.
ALTER TABLE engineer_events DROP CONSTRAINT IF EXISTS engineer_events_session_id_fkey;

CREATE TABLE IF NOT EXISTS ai_audit_logs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    trace_id TEXT NOT NULL,
    mode TEXT NOT NULL,
    provider_model TEXT NOT NULL DEFAULT '',
    prompt_template_version TEXT NOT NULL DEFAULT '',
    source_event_ids TEXT[] NOT NULL DEFAULT '{}',
    constraint_summary TEXT NOT NULL DEFAULT '',
    response_summary TEXT NOT NULL DEFAULT '',
    response_summary_limited BOOLEAN NOT NULL DEFAULT false,
    prompt_token_count INTEGER NOT NULL DEFAULT 0,
    completion_token_count INTEGER NOT NULL DEFAULT 0,
    total_token_count INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    redaction_verified BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ai_audit_logs_session_id_idx ON ai_audit_logs(session_id);
CREATE INDEX IF NOT EXISTS ai_audit_logs_created_at_idx ON ai_audit_logs(created_at);
CREATE INDEX IF NOT EXISTS ai_audit_logs_trace_id_idx ON ai_audit_logs(trace_id);
