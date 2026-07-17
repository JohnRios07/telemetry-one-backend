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
	if db.execs[0].args[0] != "lap_session-1_2" || db.execs[0].args[1] != "session-1" || db.execs[0].args[2] != 2 {
		t.Fatalf("unexpected insert args: %+v", db.execs[0].args)
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
