package sessions

import (
	"context"
	"errors"
	"testing"
	"time"

	"telemetry-one-backend/internal/telemetry"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeSummaryDB struct {
	rows      pgx.Rows
	queryErr  error
	queryRows int
}

func (db *fakeSummaryDB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	db.queryRows++
	if db.queryErr != nil {
		return nil, db.queryErr
	}
	return db.rows, nil
}

func (db *fakeSummaryDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return &fakeSummaryRow{}
}

type fakeSummaryRow struct{}

func (r *fakeSummaryRow) Scan(...any) error { return pgx.ErrNoRows }

type fakeSummaryRows struct {
	row    fakeSummaryRowData
	index  int
	closed bool
	err    error
}

type fakeSummaryRowData struct {
	ID               string
	Source           string
	Game             string
	Platform         string
	DriverAlias      string
	TrackID          string
	LayoutID         string
	StartedAt        time.Time
	EndedAt          *time.Time
	FrameBatches     int
	PersistedFrames  int
	EventCount       int
	DetectedTrackID  string
	DetectedLayoutID string
}

func (r *fakeSummaryRows) Close()                                       { r.closed = true }
func (r *fakeSummaryRows) Err() error                                   { return r.err }
func (r *fakeSummaryRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeSummaryRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeSummaryRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeSummaryRows) RawValues() [][]byte                          { return nil }
func (r *fakeSummaryRows) Conn() *pgx.Conn                              { return nil }

func (r *fakeSummaryRows) Next() bool {
	if r.index > 0 {
		return false
	}
	r.index++
	return true
}

func (r *fakeSummaryRows) Scan(dest ...any) error {
	if r.index == 0 {
		return errors.New("scan called before Next")
	}
	if r.index > 1 {
		return errors.New("no current row")
	}
	row := r.row
	*(dest[0].(*string)) = row.ID
	*(dest[1].(*string)) = row.Source
	*(dest[2].(*string)) = row.Game
	*(dest[3].(*string)) = row.Platform
	*(dest[4].(*string)) = row.DriverAlias
	*(dest[5].(*string)) = row.TrackID
	*(dest[6].(*string)) = row.LayoutID
	*(dest[7].(*time.Time)) = row.StartedAt
	*(dest[8].(**time.Time)) = row.EndedAt
	*(dest[9].(*int)) = row.FrameBatches
	*(dest[10].(*int)) = row.PersistedFrames
	*(dest[11].(*int)) = row.EventCount
	*(dest[12].(*string)) = row.DetectedTrackID
	*(dest[13].(*string)) = row.DetectedLayoutID
	return nil
}

type trackingRejectionStore struct {
	rows   *fakeSummaryRows
	called bool
}

func (s *trackingRejectionStore) Append(context.Context, telemetry.RejectionSummaryRecord) error {
	return nil
}

func (s *trackingRejectionStore) Summary(context.Context, string) (telemetry.RejectedSummaryAggregate, error) {
	return telemetry.RejectedSummaryAggregate{}, nil
}

func (s *trackingRejectionStore) Summaries(_ context.Context, sessionIDs []string) (map[string]telemetry.RejectedSummaryAggregate, error) {
	s.called = true
	if !s.rows.closed {
		return nil, errors.New("rows still open")
	}
	result := make(map[string]telemetry.RejectedSummaryAggregate, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		result[sessionID] = telemetry.RejectedSummaryAggregate{RejectedFrames: 2}
	}
	return result, nil
}

func TestPostgresSummaryRepositoryListClosesRowsBeforeLoadingRejections(t *testing.T) {
	startedAt := time.Unix(100, 0).UTC()
	rows := &fakeSummaryRows{row: fakeSummaryRowData{ID: "session-1", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: startedAt}}
	rejectionStore := &trackingRejectionStore{rows: rows}
	repo := &PostgresSummaryRepository{pool: &fakeSummaryDB{rows: rows}, rejectionStore: rejectionStore}

	resp, err := repo.List(context.Background(), SummaryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if !rows.closed {
		t.Fatal("expected rows to be closed before loading rejection summaries")
	}
	if !rejectionStore.called {
		t.Fatal("expected rejection summaries to be loaded")
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(resp.Sessions))
	}
	if resp.Sessions[0].RejectedFrames != 2 {
		t.Fatalf("expected rejection count to be merged, got %+v", resp.Sessions[0])
	}
}
