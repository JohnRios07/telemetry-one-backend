CREATE TABLE IF NOT EXISTS ingest_rejection_summaries (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    received_frames INTEGER NOT NULL,
    accepted_frames INTEGER NOT NULL,
    rejected_frames INTEGER NOT NULL,
    accepted_from_unix_ms BIGINT NOT NULL DEFAULT 0,
    accepted_to_unix_ms BIGINT NOT NULL DEFAULT 0,
    summary_jsonb JSONB NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ingest_rejection_summaries_session_id
    ON ingest_rejection_summaries(session_id);

CREATE INDEX IF NOT EXISTS idx_ingest_rejection_summaries_session_ingested_at
    ON ingest_rejection_summaries(session_id, ingested_at);
