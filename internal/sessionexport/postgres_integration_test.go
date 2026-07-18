//go:build integration

package sessionexport_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"telemetry-one-backend/internal/sessionexport"
	"telemetry-one-backend/internal/sessions"
	telemetry "telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/migrations"
)

func TestExporterIntegration_PreservesPersistedOrder(t *testing.T) {
	pool, cleanup := setupPostgresTest(t)
	defer cleanup()
	ctx := context.Background()

	repo := sessions.NewPostgresRepository(pool)
	created, err := repo.Create(ctx, sessions.Session{ID: "export-session", Source: "test", Game: "gt7", Platform: "ps5", StartedAt: time.UnixMilli(1720656000000).UTC()})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	store := telemetry.NewPostgresStore(pool)
	first := validFrame()
	second := withFrame(func(frame *telemetry.Frame) {
		frame.TimestampUnixMs = first.TimestampUnixMs + 10
		frame.LapNumber = first.LapNumber + 1
	})
	if err := store.Append(ctx, created.ID, []telemetry.Frame{first, second}); err != nil {
		t.Fatalf("append frames: %v", err)
	}
	if _, err := repo.End(ctx, created.ID, time.UnixMilli(1720656123456).UTC()); err != nil {
		t.Fatalf("finish session: %v", err)
	}

	exporter := sessionexport.Exporter{SessionReader: repo, FrameReader: store}
	result, err := exporter.Export(ctx, created.ID, nil)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	data, err := json.Marshal(result.Request)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded telemetry.IngestBatchRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Frames) != 2 || decoded.Frames[0].TimestampUnixMs != first.TimestampUnixMs || decoded.Frames[1].TimestampUnixMs != second.TimestampUnixMs {
		t.Fatalf("persisted order changed: %#v", decoded.Frames)
	}
}

func validFrame() telemetry.Frame {
	return telemetry.Frame{
		TimestampUnixMs: 1720656000000,
		SpeedMps:        58.33,
		RPM:             7100,
		Gear:            4,
		Throttle:        0.7,
		Brake:           0.2,
		Steering:        -0.12,
		FuelLiters:      38.4,
		PositionX:       123.4,
		PositionY:       5.6,
		PositionZ:       789.1,
		YawRadians:      ptrFloat64(1.57),
		YawRate:         ptrFloat64(0.03),
		WheelSpeedFL:    ptrFloat64(58.1),
		WheelSpeedFR:    ptrFloat64(58.2),
		WheelSpeedRL:    ptrFloat64(58.4),
		WheelSpeedRR:    ptrFloat64(58.3),
		LapNumber:       1,
		CurrentLapMs:    81234,
		LastLapMs:       ptrInt64(91345),
		BestLapMs:       ptrInt64(90210),
		IsOnTrack:       true,
	}
}

func withFrame(change func(*telemetry.Frame)) telemetry.Frame {
	frame := validFrame()
	change(&frame)
	return frame
}

func ptrFloat64(value float64) *float64 { return &value }

func ptrInt64(value int64) *int64 { return &value }

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
