package telemetry

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type frameExecCall struct {
	sql  string
	args []any
}

type fakeFrameDB struct {
	execs    []frameExecCall
	execErr  error
	queryErr error
}

func (db *fakeFrameDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	db.execs = append(db.execs, frameExecCall{sql: sql, args: args})
	if db.execErr != nil {
		return pgconn.CommandTag{}, db.execErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (db *fakeFrameDB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, db.queryErr
}

func TestPostgresStoreAppendReturnsExecError(t *testing.T) {
	dbErr := errors.New("db unavailable")
	store := &PostgresStore{pool: &fakeFrameDB{execErr: dbErr}, timeout: defaultTestTimeout}

	err := store.Append(context.Background(), "session-1", []Frame{validFrame()})

	if !errors.Is(err, dbErr) {
		t.Fatalf("expected db error to be returned, got %v", err)
	}
}

func TestPostgresStoreAppendSplitsBatchesByLap(t *testing.T) {
	db := &fakeFrameDB{}
	store := &PostgresStore{pool: db, timeout: defaultTestTimeout}
	first := validFrame()
	second := withFrame(func(frame *Frame) {
		frame.TimestampUnixMs++
		frame.LapNumber = first.LapNumber + 1
	})

	if err := store.Append(context.Background(), "session-1", []Frame{first, second}); err != nil {
		t.Fatalf("append frames: %v", err)
	}

	if len(db.execs) != 2 {
		t.Fatalf("expected 2 lap-scoped inserts, got %d", len(db.execs))
	}
	if db.execs[0].args[2] != first.LapNumber || db.execs[1].args[2] != second.LapNumber {
		t.Fatalf("expected each insert to keep its lap number, got args %#v and %#v", db.execs[0].args, db.execs[1].args)
	}
	if !strings.Contains(db.execs[0].sql, "hashtextextended($1, 0)") {
		t.Fatalf("expected 64-bit advisory lock hash, got SQL %s", db.execs[0].sql)
	}
}

func TestPostgresStoreFramesReturnsQueryError(t *testing.T) {
	dbErr := errors.New("query failed")
	store := &PostgresStore{pool: &fakeFrameDB{queryErr: dbErr}, timeout: defaultTestTimeout}

	_, err := store.Frames(context.Background(), "session-1")

	if !errors.Is(err, dbErr) {
		t.Fatalf("expected query error to be returned, got %v", err)
	}
}

const defaultTestTimeout = 0
