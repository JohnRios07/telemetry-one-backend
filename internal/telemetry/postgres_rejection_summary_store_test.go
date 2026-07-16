package telemetry

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type rejectionExecCall struct {
	sql  string
	args []any
}

type fakeRejectionDB struct {
	execs    []rejectionExecCall
	execErr  error
	queryErr error
	rows     pgx.Rows
}

func (db *fakeRejectionDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	db.execs = append(db.execs, rejectionExecCall{sql: sql, args: args})
	if db.execErr != nil {
		return pgconn.CommandTag{}, db.execErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (db *fakeRejectionDB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	if db.queryErr != nil {
		return nil, db.queryErr
	}
	return db.rows, nil
}

func TestPostgresRejectionSummaryStoreAppendWritesCompactSummaryJSON(t *testing.T) {
	db := &fakeRejectionDB{}
	store := &PostgresRejectionSummaryStore{pool: db, timeout: defaultTestTimeout}

	err := store.Append(context.Background(), RejectionSummaryRecord{
		SessionID:          "session-1",
		Status:             "partial",
		ReceivedFrames:     2,
		AcceptedFrames:     1,
		RejectedFrames:     1,
		AcceptedFromUnixMs: 100,
		AcceptedToUnixMs:   100,
		Summary:            RejectionSummary{Reasons: []RejectionReasonCount{{Code: "invalid_throttle", Count: 1}}},
	})
	if err != nil {
		t.Fatalf("append summary: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("expected one insert, got %d", len(db.execs))
	}
	if !strings.Contains(db.execs[0].sql, "ingest_rejection_summaries") {
		t.Fatalf("expected ingest rejection insert SQL, got %s", db.execs[0].sql)
	}
	if db.execs[0].args[1] != "session-1" || db.execs[0].args[2] != "partial" || db.execs[0].args[5] != 1 {
		t.Fatalf("unexpected insert args: %#v", db.execs[0].args)
	}
	if !strings.Contains(string(db.execs[0].args[8].([]byte)), "invalid_throttle") {
		t.Fatalf("expected compact summary JSON arg, got %#v", db.execs[0].args[8])
	}
}

func TestPostgresRejectionSummaryStoreAppendReturnsExecError(t *testing.T) {
	dbErr := errors.New("db down")
	store := &PostgresRejectionSummaryStore{pool: &fakeRejectionDB{execErr: dbErr}, timeout: defaultTestTimeout}

	err := store.Append(context.Background(), RejectionSummaryRecord{SessionID: "session-1", RejectedFrames: 1})

	if !errors.Is(err, dbErr) {
		t.Fatalf("expected db error, got %v", err)
	}
}

func TestPostgresRejectionSummaryStoreSummariesAggregatesRows(t *testing.T) {
	rows := &fakeRejectionRows{rows: []fakeRejectionRow{
		{sessionID: "session-1", rejectedFrames: 1, raw: []byte(`{"reasons":[{"code":"invalid_speed","count":1}]}`)},
		{sessionID: "session-1", rejectedFrames: 2, raw: []byte(`{"reasons":[{"code":"invalid_throttle","count":2}]}`)},
		{sessionID: "session-2", rejectedFrames: 1, raw: []byte(`{"reasons":[{"code":"invalid_brake","count":1}]}`)},
	}}
	store := &PostgresRejectionSummaryStore{pool: &fakeRejectionDB{rows: rows}, timeout: defaultTestTimeout}

	aggregates, err := store.Summaries(context.Background(), []string{"session-1", "session-2", "session-3"})
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	if aggregates["session-1"].RejectedFrames != 3 || aggregates["session-2"].RejectedFrames != 1 || aggregates["session-3"].RejectedFrames != 0 {
		t.Fatalf("unexpected aggregates: %+v", aggregates)
	}
	assertReasons(t, aggregates["session-1"].Summary.Reasons, []RejectionReasonCount{{Code: "invalid_throttle", Count: 2}, {Code: "invalid_speed", Count: 1}})
}

func TestPostgresRejectionSummaryStoreSummariesReturnsQueryError(t *testing.T) {
	dbErr := errors.New("query failed")
	store := &PostgresRejectionSummaryStore{pool: &fakeRejectionDB{queryErr: dbErr}, timeout: defaultTestTimeout}

	_, err := store.Summaries(context.Background(), []string{"session-1"})

	if !errors.Is(err, dbErr) {
		t.Fatalf("expected query error, got %v", err)
	}
}

type fakeRejectionRow struct {
	sessionID      string
	rejectedFrames int
	raw            []byte
}

type fakeRejectionRows struct {
	rows   []fakeRejectionRow
	index  int
	closed bool
	err    error
}

func (r *fakeRejectionRows) Close()                                       { r.closed = true }
func (r *fakeRejectionRows) Err() error                                   { return r.err }
func (r *fakeRejectionRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRejectionRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRejectionRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRejectionRows) RawValues() [][]byte                          { return nil }
func (r *fakeRejectionRows) Conn() *pgx.Conn                              { return nil }

func (r *fakeRejectionRows) Next() bool {
	if r.index >= len(r.rows) {
		r.closed = true
		return false
	}
	r.index++
	return true
}

func (r *fakeRejectionRows) Scan(dest ...any) error {
	row := r.rows[r.index-1]
	*(dest[0].(*string)) = row.sessionID
	*(dest[1].(*int)) = row.rejectedFrames
	*(dest[2].(*[]byte)) = row.raw
	return nil
}
