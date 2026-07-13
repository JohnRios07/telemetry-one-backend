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
	tx       *fakeFrameTx
	beginErr error
	begins   int
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

func (db *fakeFrameDB) Begin(_ context.Context) (postgresFrameTx, error) {
	db.begins++
	if db.beginErr != nil {
		return nil, db.beginErr
	}
	if db.tx == nil {
		db.tx = &fakeFrameTx{}
	}
	return db.tx, nil
}

type fakeFrameTx struct {
	execs     []frameExecCall
	execErrs  []error
	commits   int
	rollbacks int
}

func (tx *fakeFrameTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.execs = append(tx.execs, frameExecCall{sql: sql, args: args})
	if len(tx.execErrs) >= len(tx.execs) && tx.execErrs[len(tx.execs)-1] != nil {
		return pgconn.CommandTag{}, tx.execErrs[len(tx.execs)-1]
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (tx *fakeFrameTx) Commit(_ context.Context) error {
	tx.commits++
	return nil
}

func (tx *fakeFrameTx) Rollback(_ context.Context) error {
	tx.rollbacks++
	return nil
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
	store := &PostgresStore{pool: db, begin: db.Begin, timeout: defaultTestTimeout}
	first := validFrame()
	second := withFrame(func(frame *Frame) {
		frame.TimestampUnixMs++
		frame.LapNumber = first.LapNumber + 1
	})

	if err := store.Append(context.Background(), "session-1", []Frame{first, second}); err != nil {
		t.Fatalf("append frames: %v", err)
	}

	if db.begins != 1 {
		t.Fatalf("expected one transaction begin, got %d", db.begins)
	}
	if db.tx == nil || len(db.tx.execs) != 2 {
		t.Fatalf("expected 2 lap-scoped inserts in transaction, got %d", len(db.tx.execs))
	}
	if db.tx.execs[0].args[2] != first.LapNumber || db.tx.execs[1].args[2] != second.LapNumber {
		t.Fatalf("expected each insert to keep its lap number, got args %#v and %#v", db.tx.execs[0].args, db.tx.execs[1].args)
	}
	if !strings.Contains(db.tx.execs[0].sql, "hashtextextended($1, 0)") {
		t.Fatalf("expected 64-bit advisory lock hash, got SQL %s", db.tx.execs[0].sql)
	}
	if db.tx.commits != 1 {
		t.Fatalf("expected transaction commit, got %d", db.tx.commits)
	}
}

func TestPostgresStoreAppendRollsBackMultiLapTransactionWhenSecondInsertFails(t *testing.T) {
	dbErr := errors.New("second insert failed")
	tx := &fakeFrameTx{execErrs: []error{nil, dbErr}}
	db := &fakeFrameDB{tx: tx}
	store := &PostgresStore{pool: db, begin: db.Begin, timeout: defaultTestTimeout}
	first := validFrame()
	second := withFrame(func(frame *Frame) {
		frame.TimestampUnixMs++
		frame.LapNumber = first.LapNumber + 1
	})

	err := store.Append(context.Background(), "session-1", []Frame{first, second})

	if !errors.Is(err, dbErr) {
		t.Fatalf("expected second insert error, got %v", err)
	}
	if db.begins != 1 {
		t.Fatalf("expected one transaction begin, got %d", db.begins)
	}
	if len(tx.execs) != 2 {
		t.Fatalf("expected both inserts to be attempted in transaction, got %d", len(tx.execs))
	}
	if tx.commits != 0 {
		t.Fatalf("expected no commit after insert failure, got %d", tx.commits)
	}
	if tx.rollbacks != 1 {
		t.Fatalf("expected rollback after insert failure, got %d", tx.rollbacks)
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
