package laps

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeLapsDB struct {
	execs   []lapExecCall
	execErr error
}

type lapExecCall struct {
	sql  string
	args []any
}

func (db *fakeLapsDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	db.execs = append(db.execs, lapExecCall{sql: sql, args: args})
	if db.execErr != nil {
		return pgconn.CommandTag{}, db.execErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (db *fakeLapsDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

func TestPostgresRepositoryUpsertUsesSessionLapConflict(t *testing.T) {
	db := &fakeLapsDB{}
	repo := NewPostgresRepository(nil)
	repo.pool = db

	if err := repo.UpsertCompleted(context.Background(), []CompletedLap{{SessionID: "session-1", LapNumber: 2, LapTimeMs: 91234, CompletedAtUnixMs: 12345}}); err != nil {
		t.Fatalf("upsert completed lap: %v", err)
	}

	if len(db.execs) != 1 {
		t.Fatalf("expected one upsert, got %d", len(db.execs))
	}
	if !strings.Contains(db.execs[0].sql, "ON CONFLICT (session_id, lap_number)") {
		t.Fatalf("expected idempotent session/lap conflict clause, got %s", db.execs[0].sql)
	}
	if !strings.Contains(db.execs[0].sql, "telemetry_gap_count") {
		t.Fatalf("expected telemetry_gap_count in upsert SQL, got %s", db.execs[0].sql)
	}
	if db.execs[0].args[0] != "lap_session-1_2" || db.execs[0].args[1] != "session-1" || db.execs[0].args[2] != 2 || db.execs[0].args[7] != 0 {
		t.Fatalf("unexpected insert args: %+v", db.execs[0].args)
	}
}

func TestPostgresRepositoryUpdateTelemetryGapCountOverwrites(t *testing.T) {
	db := &fakeLapsDB{}
	repo := NewPostgresRepository(nil)
	repo.pool = db

	if err := repo.UpdateTelemetryGapCount(context.Background(), "session-1", 2, 3); err != nil {
		t.Fatalf("update telemetry gap count: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("expected one update, got %d", len(db.execs))
	}
	if !strings.Contains(db.execs[0].sql, "SET telemetry_gap_count = $3") || db.execs[0].args[0] != "session-1" || db.execs[0].args[1] != 2 || db.execs[0].args[2] != 3 {
		t.Fatalf("unexpected update call: sql=%s args=%+v", db.execs[0].sql, db.execs[0].args)
	}
}

func TestPostgresRepositoryRejectsNegativeTelemetryGapCount(t *testing.T) {
	repo := NewPostgresRepository(nil)
	repo.pool = &fakeLapsDB{}

	if err := repo.UpsertCompleted(context.Background(), []CompletedLap{{SessionID: "session-1", LapNumber: 2, LapTimeMs: 91234, CompletedAtUnixMs: 12345, TelemetryGapCount: -1}}); err != ErrInvalidGapCount {
		t.Fatalf("expected ErrInvalidGapCount on upsert, got %v", err)
	}
	if err := repo.UpdateTelemetryGapCount(context.Background(), "session-1", 2, -1); err != ErrInvalidGapCount {
		t.Fatalf("expected ErrInvalidGapCount on update, got %v", err)
	}
}

func TestPostgresRepositoryUpsertReturnsExecError(t *testing.T) {
	dbErr := errors.New("insert failed")
	repo := NewPostgresRepository(nil)
	repo.pool = &fakeLapsDB{execErr: dbErr}

	err := repo.UpsertCompleted(context.Background(), []CompletedLap{{SessionID: "session-1", LapNumber: 2, LapTimeMs: 91234, CompletedAtUnixMs: 12345}})
	if !errors.Is(err, dbErr) {
		t.Fatalf("expected exec error, got %v", err)
	}
}

func TestPostgresRepositoryUpsertAcceptsZeroBestLapMs(t *testing.T) {
	zero := int64(0)
	db := &fakeLapsDB{}
	repo := NewPostgresRepository(nil)
	repo.pool = db

	if err := repo.UpsertCompleted(context.Background(), []CompletedLap{{SessionID: "session-1", LapNumber: 2, LapTimeMs: 91234, CompletedAtUnixMs: 12345, BestLapMs: &zero}}); err != nil {
		t.Fatalf("upsert completed lap with zero best lap ms: %v", err)
	}

	if len(db.execs) != 1 {
		t.Fatalf("expected one upsert, got %d", len(db.execs))
	}
	if db.execs[0].args[5] == nil {
		t.Fatalf("expected best lap arg to be preserved, got nil")
	}
	if got := db.execs[0].args[5].(*int64); got == nil || *got != 0 {
		t.Fatalf("expected zero best lap arg, got %+v", db.execs[0].args[5])
	}
}

func TestPostgresRepositoryUpsertSamplesUsesLapDistanceConflictAndNullPointers(t *testing.T) {
	zero := 0.0
	db := &fakeLapsDB{}
	repo := NewPostgresRepository(nil)
	repo.pool = db

	if err := repo.UpsertSamples(context.Background(), []LapSample{{LapID: "lap-1", DistanceMeters: 5, Source: LapSampleSourceExplicitLapDistance, SpeedMps: &zero, YawRate: nil}}); err != nil {
		t.Fatalf("upsert sample: %v", err)
	}

	if len(db.execs) != 1 {
		t.Fatalf("expected one upsert, got %d", len(db.execs))
	}
	if !strings.Contains(db.execs[0].sql, "ON CONFLICT (lap_id, distance_meters) DO NOTHING") {
		t.Fatalf("expected idempotent lap/distance conflict clause, got %s", db.execs[0].sql)
	}
	if db.execs[0].args[0] != "lap_sample_lap-1_5" || db.execs[0].args[1] != "lap-1" || db.execs[0].args[2] != 5 {
		t.Fatalf("unexpected insert args: %+v", db.execs[0].args)
	}
	if db.execs[0].args[5] == nil || *db.execs[0].args[5].(*float64) != 0 {
		t.Fatalf("expected explicit zero speed arg, got %+v", db.execs[0].args[5])
	}
	if got, ok := db.execs[0].args[13].(*float64); !ok || got != nil {
		t.Fatalf("expected nil yaw rate arg, got %+v", db.execs[0].args[13])
	}
}
