//go:build integration

package sessions

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupSummaryPGTest(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("telemetry_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategyAndDeadline(60*time.Second, wait.ForLog(".*database system is ready to accept connections.*").AsRegexp()),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("get connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("create pool: %v", err)
	}

	var pingErr error
	for i := 0; i < 5; i++ {
		pingErr = pool.Ping(ctx)
		if pingErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if pingErr != nil {
		pool.Close()
		container.Terminate(ctx)
		t.Fatalf("ping postgres: %v", pingErr)
	}

	if err := migrateSummaryTestDB(ctx, pool); err != nil {
		pool.Close()
		container.Terminate(ctx)
		t.Fatalf("run migrations: %v", err)
	}

	cleaned := false
	cleanup := func() {
		if cleaned {
			return
		}
		cleaned = true
		pool.Close()
		container.Terminate(ctx)
	}

	return pool, cleanup
}

func migrateSummaryTestDB(ctx context.Context, pool *pgxpool.Pool) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			game TEXT NOT NULL,
			platform TEXT NOT NULL,
			driver_alias TEXT NOT NULL DEFAULT '',
			track_id TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ NOT NULL,
			ended_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS frame_batches (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			lap_number INTEGER NOT NULL CHECK (lap_number >= 0),
			batch_index INTEGER NOT NULL CHECK (batch_index >= 0),
			frame_count INTEGER NOT NULL CHECK (frame_count > 0),
			frames_jsonb JSONB NOT NULL,
			time_start_ms BIGINT NOT NULL,
			time_end_ms BIGINT NOT NULL,
			ingested_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS frame_batches_session_id_idx ON frame_batches(session_id)`,
		`CREATE TABLE IF NOT EXISTS engineer_events (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			lap_number INTEGER NOT NULL DEFAULT 0,
			corner_id TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL,
			severity TEXT NOT NULL,
			timestamp_ms BIGINT NOT NULL,
			metrics JSONB NOT NULL DEFAULT '[]',
			metadata JSONB NOT NULL DEFAULT '{}'
		)`,
		`CREATE TABLE IF NOT EXISTS ai_audit_logs (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			trace_id TEXT NOT NULL,
			mode TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	}
	for _, stmt := range statements {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func TestPostgresIntegration_SummaryListEmpty(t *testing.T) {
	pool, cleanup := setupSummaryPGTest(t)
	defer cleanup()
	ctx := context.Background()

	repo := NewPostgresSummaryRepository(pool)
	resp, err := repo.List(ctx, SummaryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(resp.Sessions))
	}
}

func TestPostgresIntegration_SummaryListWithData(t *testing.T) {
	pool, cleanup := setupSummaryPGTest(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `INSERT INTO sessions (id, source, game, platform, driver_alias, track_id, started_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		"session-a", "flutter", "gt7", "ps5", "alex", "gt7_watkins_glen_international", now)
	if err != nil {
		t.Fatalf("insert session a: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO sessions (id, source, game, platform, started_at, ended_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		"session-b", "unity", "gt7", "ps5", now.Add(-time.Hour), now.Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("insert session b: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO frame_batches (id, session_id, lap_number, batch_index, frame_count, frames_jsonb, time_start_ms, time_end_ms) VALUES ($1, $2, 1, 0, 5, '[]', 100, 500)`,
		"batch-1", "session-a")
	if err != nil {
		t.Fatalf("insert batch 1: %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO frame_batches (id, session_id, lap_number, batch_index, frame_count, frames_jsonb, time_start_ms, time_end_ms) VALUES ($1, $2, 2, 1, 3, '[]', 600, 900)`,
		"batch-2", "session-a")
	if err != nil {
		t.Fatalf("insert batch 2: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO engineer_events (id, session_id, lap_number, type, severity, timestamp_ms, metrics, metadata) VALUES ($1, $2, 1, 'late_throttle', 'medium', 300, '[]', '{}')`,
		"event-1", "session-a")
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO ai_audit_logs (id, session_id, trace_id, mode) VALUES ($1, $2, 'trace-1', 'auto')`,
		"audit-1", "session-a")
	if err != nil {
		t.Fatalf("insert audit log: %v", err)
	}

	repo := NewPostgresSummaryRepository(pool)

	t.Run("list all", func(t *testing.T) {
		resp, err := repo.List(ctx, SummaryFilter{Limit: 10})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Sessions) != 2 {
			t.Fatalf("expected 2 sessions, got %d", len(resp.Sessions))
		}
		if resp.Sessions[0].ID != "session-a" {
			t.Fatalf("expected session-a first (most recent), got %q", resp.Sessions[0].ID)
		}
		if resp.Sessions[0].Status != StatusActive {
			t.Fatalf("expected active, got %s", resp.Sessions[0].Status)
		}
		if resp.Sessions[0].Source != "flutter" {
			t.Fatalf("expected source flutter, got %q", resp.Sessions[0].Source)
		}
		if resp.Sessions[0].FrameBatches != 2 {
			t.Fatalf("expected 2 frame batches, got %d", resp.Sessions[0].FrameBatches)
		}
		if resp.Sessions[0].PersistedFrames != 8 {
			t.Fatalf("expected 8 persisted frames, got %d", resp.Sessions[0].PersistedFrames)
		}
		if resp.Sessions[0].EventCount != 1 {
			t.Fatalf("expected 1 event, got %d", resp.Sessions[0].EventCount)
		}
		if resp.Sessions[0].DriverAlias != "alex" {
			t.Fatalf("expected driverAlias, got %q", resp.Sessions[0].DriverAlias)
		}
		if resp.Sessions[0].TrackID != "gt7_watkins_glen_international" {
			t.Fatalf("expected trackId, got %q", resp.Sessions[0].TrackID)
		}

		if resp.Sessions[1].Status != StatusFinished {
			t.Fatalf("expected finished, got %s", resp.Sessions[1].Status)
		}
		if resp.Sessions[1].EndedAt == nil || resp.Sessions[1].DurationMs == nil {
			t.Fatal("expected endedAt and durationMs for finished session")
		}
		if resp.Sessions[1].FrameBatches != 0 || resp.Sessions[1].PersistedFrames != 0 {
			t.Fatalf("expected 0 frames for session-b, got batches=%d frames=%d",
				resp.Sessions[1].FrameBatches, resp.Sessions[1].PersistedFrames)
		}
	})

	t.Run("list limit", func(t *testing.T) {
		resp, err := repo.List(ctx, SummaryFilter{Limit: 1})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Sessions) != 1 {
			t.Fatalf("expected 1 session, got %d", len(resp.Sessions))
		}
		if resp.Sessions[0].ID != "session-a" {
			t.Fatalf("expected session-a, got %q", resp.Sessions[0].ID)
		}
	})
}

func TestPostgresIntegration_SummaryDetail(t *testing.T) {
	pool, cleanup := setupSummaryPGTest(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `INSERT INTO sessions (id, source, game, platform, driver_alias, track_id, started_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		"session-detail", "flutter", "gt7", "ps5", "alex", "gt7_watkins_glen_international", now)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO frame_batches (id, session_id, lap_number, batch_index, frame_count, frames_jsonb, time_start_ms, time_end_ms) VALUES ($1, $2, 1, 0, 5, '[]', 100, 500)`,
		"batch-d1", "session-detail")
	if err != nil {
		t.Fatalf("insert batch 1: %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO frame_batches (id, session_id, lap_number, batch_index, frame_count, frames_jsonb, time_start_ms, time_end_ms) VALUES ($1, $2, 1, 1, 3, '[]', 600, 800)`,
		"batch-d2", "session-detail")
	if err != nil {
		t.Fatalf("insert batch 2: %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO frame_batches (id, session_id, lap_number, batch_index, frame_count, frames_jsonb, time_start_ms, time_end_ms) VALUES ($1, $2, 2, 2, 4, '[]', 900, 1200)`,
		"batch-d3", "session-detail")
	if err != nil {
		t.Fatalf("insert batch 3: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO engineer_events (id, session_id, lap_number, type, severity, timestamp_ms, metrics, metadata) VALUES ($1, $2, 1, 'late_throttle', 'medium', 300, '[]', '{}')`,
		"event-d1", "session-detail")
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO ai_audit_logs (id, session_id, trace_id, mode) VALUES ($1, $2, 'trace-1', 'auto')`,
		"audit-d1", "session-detail")
	if err != nil {
		t.Fatalf("insert audit log: %v", err)
	}

	repo := NewPostgresSummaryRepository(pool)

	summary, err := repo.Summary(ctx, "session-detail")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if summary.Session.ID != "session-detail" {
		t.Fatalf("expected session-detail, got %s", summary.Session.ID)
	}
	if summary.Session.Source != "flutter" || summary.Session.Game != "gt7" || summary.Session.Platform != "ps5" {
		t.Fatalf("unexpected metadata: %+v", summary.Session)
	}
	if summary.Session.DriverAlias != "alex" {
		t.Fatalf("expected driverAlias, got %q", summary.Session.DriverAlias)
	}
	if summary.Session.TrackID != "gt7_watkins_glen_international" {
		t.Fatalf("expected trackId, got %q", summary.Session.TrackID)
	}
	if summary.Session.FrameBatches != summary.FrameBatches {
		t.Fatalf("expected nested frame batches %d, got %d", summary.FrameBatches, summary.Session.FrameBatches)
	}
	if summary.Session.PersistedFrames != summary.PersistedFrames {
		t.Fatalf("expected nested persisted frames %d, got %d", summary.PersistedFrames, summary.Session.PersistedFrames)
	}
	if summary.Session.EventCount != summary.EngineerEventCount {
		t.Fatalf("expected nested engineer event count %d, got %d", summary.EngineerEventCount, summary.Session.EventCount)
	}
	if summary.Session.Status != StatusActive {
		t.Fatalf("expected active, got %s", summary.Session.Status)
	}
	if summary.FrameBatches != 3 {
		t.Fatalf("expected 3 frame batches, got %d", summary.FrameBatches)
	}
	if summary.PersistedFrames != 12 {
		t.Fatalf("expected 12 persisted frames (5+3+4), got %d", summary.PersistedFrames)
	}
	if summary.LapsDetected != 2 {
		t.Fatalf("expected 2 laps detected, got %d", summary.LapsDetected)
	}
	if summary.TimeRangeMs == nil || summary.TimeRangeMs.From != 100 || summary.TimeRangeMs.To != 1200 {
		t.Fatalf("expected time range [100, 1200], got %+v", summary.TimeRangeMs)
	}
	if summary.EngineerEventCount != 1 {
		t.Fatalf("expected 1 engineer event, got %d", summary.EngineerEventCount)
	}
	if summary.AIAuditLogCount != 1 {
		t.Fatalf("expected 1 ai audit log, got %d", summary.AIAuditLogCount)
	}
}

func TestPostgresIntegration_SummaryNotFound(t *testing.T) {
	pool, cleanup := setupSummaryPGTest(t)
	defer cleanup()

	repo := NewPostgresSummaryRepository(pool)
	_, err := repo.Summary(context.Background(), "session-missing")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPostgresIntegration_SummaryDetailNoFrameData(t *testing.T) {
	pool, cleanup := setupSummaryPGTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO sessions (id, source, game, platform, started_at) VALUES ($1, $2, $3, $4, $5)`,
		"session-empty", "flutter", "gt7", "ps5", time.Now().UTC())
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	repo := NewPostgresSummaryRepository(pool)

	summary, err := repo.Summary(ctx, "session-empty")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if summary.FrameBatches != 0 {
		t.Fatalf("expected 0 frame batches, got %d", summary.FrameBatches)
	}
	if summary.PersistedFrames != 0 {
		t.Fatalf("expected 0 persisted frames, got %d", summary.PersistedFrames)
	}
	if summary.LapsDetected != 0 {
		t.Fatalf("expected 0 laps detected, got %d", summary.LapsDetected)
	}
	if summary.TimeRangeMs != nil {
		t.Fatalf("expected nil time range, got %+v", summary.TimeRangeMs)
	}
	if summary.EngineerEventCount != 0 {
		t.Fatalf("expected 0 events, got %d", summary.EngineerEventCount)
	}
	if summary.AIAuditLogCount != 0 {
		t.Fatalf("expected 0 audit logs, got %d", summary.AIAuditLogCount)
	}
}
