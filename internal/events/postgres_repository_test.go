package events

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type eventExecCall struct {
	args []any
}

type fakeEventsDB struct {
	execs   []eventExecCall
	execErr error
	rows    int64
}

func (db *fakeEventsDB) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	db.execs = append(db.execs, eventExecCall{args: args})
	if db.execErr != nil {
		return pgconn.CommandTag{}, db.execErr
	}
	if db.rows == 0 {
		return pgconn.NewCommandTag("INSERT 0 0"), nil
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (db *fakeEventsDB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, nil
}

func TestPostgresRepositoryAppendReturnsExecError(t *testing.T) {
	dbErr := errors.New("insert failed")
	repo := NewPostgresRepository(nil, DedupOptions{})
	repo.pool = &fakeEventsDB{execErr: dbErr}

	_, accepted, err := repo.Append(context.Background(), validEvent())

	if accepted {
		t.Fatalf("expected failed insert not to be accepted")
	}
	if !errors.Is(err, dbErr) {
		t.Fatalf("expected exec error to be returned, got %v", err)
	}
}

func TestPostgresRepositoryAppendStoresFullEventMetadata(t *testing.T) {
	db := &fakeEventsDB{rows: 1}
	repo := NewPostgresRepository(nil, DedupOptions{})
	repo.pool = db
	event := validEvent()

	_, accepted, err := repo.Append(context.Background(), event)
	if err != nil || !accepted {
		t.Fatalf("expected accepted event, accepted=%v err=%v", accepted, err)
	}

	if len(db.execs) != 1 {
		t.Fatalf("expected one insert, got %d", len(db.execs))
	}
	metadataBytes, ok := db.execs[0].args[8].([]byte)
	if !ok {
		t.Fatalf("expected metadata argument to be []byte, got %T", db.execs[0].args[8])
	}
	var metadata struct {
		Event EngineerEvent `json:"event"`
	}
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata.Event.EventID != event.EventID {
		t.Fatalf("expected full event metadata to preserve event id %q, got %q", event.EventID, metadata.Event.EventID)
	}
}
