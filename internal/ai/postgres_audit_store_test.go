package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type auditExecCall struct {
	args []any
}

type fakeAuditDB struct {
	execs   []auditExecCall
	execErr error
}

func (db *fakeAuditDB) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	db.execs = append(db.execs, auditExecCall{args: args})
	if db.execErr != nil {
		return pgconn.CommandTag{}, db.execErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestPostgresAuditStoreRecordUsesSeparateIDAndTraceID(t *testing.T) {
	db := &fakeAuditDB{}
	store := &PostgresAuditStore{pool: db}
	record := AuditRecord{TraceID: "trace-1", SessionID: "session-1", Mode: "coach"}

	if err := store.Record(context.Background(), record); err != nil {
		t.Fatalf("record audit log: %v", err)
	}

	if len(db.execs) != 1 {
		t.Fatalf("expected one insert, got %d", len(db.execs))
	}
	if db.execs[0].args[0] == record.TraceID {
		t.Fatalf("expected audit primary key id to be distinct from trace id")
	}
	if db.execs[0].args[2] != record.TraceID {
		t.Fatalf("expected trace id in third argument, got %#v", db.execs[0].args[2])
	}
}

func TestPostgresAuditStoreRecordReturnsExecError(t *testing.T) {
	dbErr := errors.New("insert failed")
	store := &PostgresAuditStore{pool: &fakeAuditDB{execErr: dbErr}}

	err := store.Record(context.Background(), AuditRecord{TraceID: "trace-1", SessionID: "session-1", Mode: "coach"})

	if !errors.Is(err, dbErr) {
		t.Fatalf("expected exec error to be returned, got %v", err)
	}
}
