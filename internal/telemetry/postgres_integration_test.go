//go:build integration

package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/migrations"
)

func migrateTestDB(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}

	files, err := migrations.UpFiles()
	if err != nil {
		return err
	}

	for _, file := range files {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", file).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}

		content, err := migrations.FS.ReadFile(file)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(content)); err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", file); err != nil {
			return err
		}
	}

	return nil
}

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

	t.Logf("connecting to %s", connStr)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("create pool: %v", err)
	}

	// Retry ping a few times since Postgres may need a moment.
	var pingErr error
	for i := 0; i < 5; i++ {
		pingErr = pool.Ping(ctx)
		if pingErr == nil {
			break
		}
		t.Logf("ping attempt %d: %v", i+1, pingErr)
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

func TestPostgresIntegration_SessionLifecycle(t *testing.T) {
	pool, cleanup := setupPostgresTest(t)
	defer cleanup()
	ctx := context.Background()

	repo := sessions.NewPostgresRepository(pool)
	created, err := repo.Create(ctx, sessions.Session{
		ID: "integration-session-1", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: time.UnixMilli(1720656000000).UTC(),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if created.Status() != sessions.StatusActive {
		t.Fatalf("expected active session, got %v", created.Status())
	}

	found, err := repo.FindByID(ctx, "integration-session-1")
	if err != nil {
		t.Fatalf("find session: %v", err)
	}
	if found.ID != "integration-session-1" {
		t.Fatalf("expected found session id match")
	}

	finished, err := repo.End(ctx, "integration-session-1", time.UnixMilli(1720656123456).UTC())
	if err != nil {
		t.Fatalf("end session: %v", err)
	}
	if finished.Status() != sessions.StatusFinished {
		t.Fatalf("expected finished session, got %v", finished.Status())
	}

	if _, err := repo.End(ctx, "integration-session-1", time.Now()); !errors.Is(err, sessions.ErrAlreadyFinished) {
		t.Fatalf("expected ErrAlreadyFinished on double finish, got %v", err)
	}
}

func TestPostgresIntegration_AppendActiveSession(t *testing.T) {
	pool, cleanup := setupPostgresTest(t)
	defer cleanup()
	ctx := context.Background()

	repo := sessions.NewPostgresRepository(pool)
	if _, err := repo.Create(ctx, sessions.Session{
		ID: "append-active", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: time.UnixMilli(1720656000000).UTC(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	store := NewPostgresStore(pool)
	if err := store.Append(ctx, "append-active", []Frame{validFrame()}); err != nil {
		t.Fatalf("append frames: %v", err)
	}

	frames, err := store.Frames(ctx, "append-active")
	if err != nil {
		t.Fatalf("get frames: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}

	t.Log("active session append: OK")
}

func TestPostgresIntegration_AppendMultiLapActiveSession(t *testing.T) {
	pool, cleanup := setupPostgresTest(t)
	defer cleanup()
	ctx := context.Background()

	repo := sessions.NewPostgresRepository(pool)
	if _, err := repo.Create(ctx, sessions.Session{
		ID: "append-multilap", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: time.UnixMilli(1720656000000).UTC(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	first := validFrame()
	second := withFrame(func(frame *Frame) {
		frame.TimestampUnixMs++
		frame.LapNumber = first.LapNumber + 1
	})

	store := NewPostgresStore(pool)
	if err := store.Append(ctx, "append-multilap", []Frame{first, second}); err != nil {
		t.Fatalf("append frames: %v", err)
	}

	frames, err := store.Frames(ctx, "append-multilap")
	if err != nil {
		t.Fatalf("get frames: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	if frames[0].LapNumber != first.LapNumber || frames[1].LapNumber != second.LapNumber {
		t.Fatalf("expected frames to retain lap numbers")
	}

	t.Log("multi-lap active session append: OK")
}

func TestPostgresIntegration_AppendNonexistentSession(t *testing.T) {
	pool, cleanup := setupPostgresTest(t)
	defer cleanup()
	ctx := context.Background()

	store := NewPostgresStore(pool)
	err := store.Append(ctx, "no-such-session", []Frame{validFrame()})
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}

	t.Log("nonexistent session rejected: OK")
}

func TestPostgresIntegration_AppendFinishedSession(t *testing.T) {
	pool, cleanup := setupPostgresTest(t)
	defer cleanup()
	ctx := context.Background()

	repo := sessions.NewPostgresRepository(pool)
	if _, err := repo.Create(ctx, sessions.Session{
		ID: "append-finished", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: time.UnixMilli(1720656000000).UTC(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := repo.End(ctx, "append-finished", time.UnixMilli(1720656123456).UTC()); err != nil {
		t.Fatalf("end session: %v", err)
	}

	store := NewPostgresStore(pool)
	err := store.Append(ctx, "append-finished", []Frame{validFrame()})
	if err == nil {
		t.Fatal("expected error for finished session")
	}
	if !errors.Is(err, ErrSessionFinished) {
		t.Fatalf("expected ErrSessionFinished, got %v", err)
	}

	t.Log("finished session rejected: OK")
}

func TestPostgresIntegration_FinishBeforeAppendNoLeak(t *testing.T) {
	pool, cleanup := setupPostgresTest(t)
	defer cleanup()
	ctx := context.Background()

	repo := sessions.NewPostgresRepository(pool)
	if _, err := repo.Create(ctx, sessions.Session{
		ID: "finish-then-append", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: time.UnixMilli(1720656000000).UTC(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := repo.End(ctx, "finish-then-append", time.UnixMilli(1720656123456).UTC()); err != nil {
		t.Fatalf("end session: %v", err)
	}

	store := NewPostgresStore(pool)
	err := store.Append(ctx, "finish-then-append", []Frame{validFrame()})
	if !errors.Is(err, ErrSessionFinished) {
		t.Fatalf("expected ErrSessionFinished, got %v", err)
	}

	frames, err := store.Frames(ctx, "finish-then-append")
	if err != nil {
		t.Fatalf("get frames: %v", err)
	}
	if len(frames) != 0 {
		t.Fatalf("expected zero frames after rejected append, got %d", len(frames))
	}

	t.Log("no frames leaked after finish-before-append: OK")
}
