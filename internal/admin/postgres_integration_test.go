//go:build integration

package admin

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupPostgresTest(t *testing.T) (*pgxpool.Pool, func()) {
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

	if err := migrateTestDB(ctx, pool); err != nil {
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

func migrateTestDB(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}

	_, err := pool.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ('integration-test-migration')
ON CONFLICT (version) DO NOTHING`)
	if err != nil {
		return err
	}

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

func TestPostgresIntegration_IngestStatsEmpty(t *testing.T) {
	pool, cleanup := setupPostgresTest(t)
	defer cleanup()
	ctx := context.Background()

	repo := NewPostgresStatsRepo(pool)
	resp, err := repo.Stats(ctx, DefaultLimit, DefaultDays)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.Mode != ModePostgres {
		t.Fatalf("expected mode postgres, got %q", resp.Mode)
	}
	if resp.Totals.Sessions != 0 || resp.Totals.ActiveSessions != 0 || resp.Totals.FinishedSessions != 0 {
		t.Fatalf("expected zero totals on empty db, got %+v", resp.Totals)
	}
	if resp.Totals.FrameBatches == nil || *resp.Totals.FrameBatches != 0 {
		t.Fatalf("expected 0 frame batches, got %v", resp.Totals.FrameBatches)
	}
	if resp.Totals.EngineerEvents == nil || *resp.Totals.EngineerEvents != 0 {
		t.Fatalf("expected 0 engineer events, got %v", resp.Totals.EngineerEvents)
	}
	if resp.Totals.AIAuditLogs == nil || *resp.Totals.AIAuditLogs != 0 {
		t.Fatalf("expected 0 ai audit logs, got %v", resp.Totals.AIAuditLogs)
	}
	if len(resp.RecentSessions) != 0 {
		t.Fatalf("expected empty recent sessions, got %d", len(resp.RecentSessions))
	}
	if len(resp.Daily) != 0 {
		t.Fatalf("expected empty daily, got %d", len(resp.Daily))
	}
}

func TestPostgresIntegration_IngestStatsWithData(t *testing.T) {
	pool, cleanup := setupPostgresTest(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `INSERT INTO sessions (id, source, game, platform, started_at) VALUES ($1, $2, $3, $4, $5)`,
		"session-a", "test", "gt7", "ps5", now)
	if err != nil {
		t.Fatalf("insert session a: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO sessions (id, source, game, platform, started_at, ended_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		"session-b", "test", "gt7", "ps5", now.Add(-time.Hour), now.Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("insert session b: %v", err)
	}

	for i := 0; i < 3; i++ {
		_, err = pool.Exec(ctx, `INSERT INTO frame_batches (id, session_id, lap_number, batch_index, frame_count, frames_jsonb, time_start_ms, time_end_ms) VALUES ($1, $2, 0, $3, 5, '[]', 1, 2)`,
			"batch-"+string(rune('a'+i)), "session-a", i)
		if err != nil {
			t.Fatalf("insert batch %d: %v", i, err)
		}
	}

	_, err = pool.Exec(ctx, `INSERT INTO engineer_events (id, session_id, lap_number, type, severity, timestamp_ms, metrics, metadata) VALUES ($1, $2, 1, 'late_throttle', 'medium', 100, '[]', '{}')`,
		"event-1", "session-a")
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO ai_audit_logs (id, session_id, trace_id, mode) VALUES ($1, $2, 'trace-1', 'auto')`,
		"audit-1", "session-a")
	if err != nil {
		t.Fatalf("insert audit log: %v", err)
	}

	repo := NewPostgresStatsRepo(pool)
	resp, err := repo.Stats(ctx, 10, 90)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.Totals.Sessions != 2 {
		t.Fatalf("expected 2 sessions, got %d", resp.Totals.Sessions)
	}
	if resp.Totals.ActiveSessions != 1 {
		t.Fatalf("expected 1 active session, got %d", resp.Totals.ActiveSessions)
	}
	if resp.Totals.FinishedSessions != 1 {
		t.Fatalf("expected 1 finished session, got %d", resp.Totals.FinishedSessions)
	}
	if resp.Totals.FrameBatches == nil || *resp.Totals.FrameBatches != 3 {
		t.Fatalf("expected 3 frame batches, got %d", *resp.Totals.FrameBatches)
	}
	if resp.Totals.PersistedFrames == nil || *resp.Totals.PersistedFrames != 15 {
		t.Fatalf("expected 15 persisted frames, got %d", *resp.Totals.PersistedFrames)
	}
	if resp.Totals.EngineerEvents == nil || *resp.Totals.EngineerEvents != 1 {
		t.Fatalf("expected 1 engineer event, got %d", *resp.Totals.EngineerEvents)
	}
	if resp.Totals.AIAuditLogs == nil || *resp.Totals.AIAuditLogs != 1 {
		t.Fatalf("expected 1 ai audit log, got %d", *resp.Totals.AIAuditLogs)
	}

	if len(resp.RecentSessions) != 2 {
		t.Fatalf("expected 2 recent sessions, got %d", len(resp.RecentSessions))
	}
	if resp.RecentSessions[0].ID != "session-a" {
		t.Fatalf("expected session-a first (most recent), got %q", resp.RecentSessions[0].ID)
	}
	if resp.RecentSessions[0].Status != "active" || resp.RecentSessions[1].Status != "finished" {
		t.Fatalf("expected statuses active then finished")
	}
	if resp.RecentSessions[0].FrameBatches != 3 || resp.RecentSessions[0].PersistedFrames != 15 {
		t.Fatalf("expected session-a to have 3 batches, 15 frames, got batches=%d frames=%d",
			resp.RecentSessions[0].FrameBatches, resp.RecentSessions[0].PersistedFrames)
	}
	if resp.RecentSessions[1].FrameBatches != 0 || resp.RecentSessions[1].PersistedFrames != 0 {
		t.Fatalf("expected session-b to have 0 batches, got batches=%d frames=%d",
			resp.RecentSessions[1].FrameBatches, resp.RecentSessions[1].PersistedFrames)
	}

	if resp.RecentSessions[0].StartedAt == "" {
		t.Fatal("expected non-empty startedAt")
	}
	if resp.RecentSessions[0].EndedAt != nil {
		t.Fatal("expected nil endedAt for active session")
	}
	if resp.RecentSessions[0].DurationMs != nil {
		t.Fatal("expected nil durationMs for active session")
	}
	if resp.RecentSessions[1].EndedAt == nil || resp.RecentSessions[1].DurationMs == nil {
		t.Fatal("expected endedAt and durationMs for finished session")
	}

	if len(resp.Daily) == 0 {
		t.Fatal("expected at least 1 daily entry")
	}
	totalDailySessions := 0
	for _, d := range resp.Daily {
		totalDailySessions += d.Sessions
	}
	if totalDailySessions != 2 {
		t.Fatalf("expected 2 sessions across daily entries, got %d", totalDailySessions)
	}
}

func TestPostgresIntegration_IngestStatsLimitRecentSessions(t *testing.T) {
	pool, cleanup := setupPostgresTest(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_, err := pool.Exec(ctx, `INSERT INTO sessions (id, source, game, platform, started_at) VALUES ($1, $2, $3, $4, $5)`,
			"s"+string(rune('a'+i)), "test", "gt7", "ps5", now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("insert session: %v", err)
		}
	}

	repo := NewPostgresStatsRepo(pool)

	resp2, err := repo.Stats(ctx, 2, 90)
	if err != nil {
		t.Fatalf("expected no error: %v", err)
	}
	if len(resp2.RecentSessions) != 2 {
		t.Fatalf("expected 2 recent sessions with limit=2, got %d", len(resp2.RecentSessions))
	}

	resp10, err := repo.Stats(ctx, 10, 90)
	if err != nil {
		t.Fatalf("expected no error: %v", err)
	}
	if len(resp10.RecentSessions) != 5 {
		t.Fatalf("expected 5 recent sessions with limit=10, got %d", len(resp10.RecentSessions))
	}
}
