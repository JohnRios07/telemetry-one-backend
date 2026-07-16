package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresRejectionDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PostgresRejectionSummaryStore struct {
	pool    postgresRejectionDB
	timeout time.Duration
}

func NewPostgresRejectionSummaryStore(pool *pgxpool.Pool) *PostgresRejectionSummaryStore {
	var db postgresRejectionDB
	if pool != nil {
		db = pool
	}
	return &PostgresRejectionSummaryStore{pool: db, timeout: 5 * time.Second}
}

func (s *PostgresRejectionSummaryStore) Append(ctx context.Context, record RejectionSummaryRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.pool == nil || record.SessionID == "" || record.RejectedFrames <= 0 {
		return nil
	}
	if record.ID == "" {
		record.ID = newRejectionSummaryID()
	}
	if record.IngestedAt.IsZero() {
		record.IngestedAt = time.Now().UTC()
	}
	body, err := json.Marshal(record.Summary)
	if err != nil {
		return fmt.Errorf("marshal rejection summary: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	_, err = s.pool.Exec(ctx, `
INSERT INTO ingest_rejection_summaries (
    id, session_id, status, received_frames, accepted_frames, rejected_frames,
    accepted_from_unix_ms, accepted_to_unix_ms, summary_jsonb, ingested_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10)`,
		record.ID, record.SessionID, record.Status, record.ReceivedFrames, record.AcceptedFrames, record.RejectedFrames,
		record.AcceptedFromUnixMs, record.AcceptedToUnixMs, body, record.IngestedAt.UTC())
	if err != nil {
		return fmt.Errorf("append rejection summary: %w", err)
	}
	return nil
}

func (s *PostgresRejectionSummaryStore) Summary(ctx context.Context, sessionID string) (RejectedSummaryAggregate, error) {
	if sessionID == "" {
		return RejectedSummaryAggregate{}, nil
	}
	results, err := s.Summaries(ctx, []string{sessionID})
	if err != nil {
		return RejectedSummaryAggregate{}, err
	}
	return results[sessionID], nil
}

func (s *PostgresRejectionSummaryStore) Summaries(ctx context.Context, sessionIDs []string) (map[string]RejectedSummaryAggregate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make(map[string]RejectedSummaryAggregate, len(sessionIDs))
	if s == nil || s.pool == nil || len(sessionIDs) == 0 {
		return result, nil
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
SELECT session_id, rejected_frames, summary_jsonb
FROM ingest_rejection_summaries
WHERE session_id = ANY($1)
ORDER BY session_id ASC, ingested_at ASC, id ASC`, sessionIDs)
	if err != nil {
		return nil, fmt.Errorf("query rejection summaries: %w", err)
	}
	defer rows.Close()

	recordsBySession := make(map[string][]RejectionSummaryRecord)
	for rows.Next() {
		var sessionID string
		var rejectedFrames int
		var raw []byte
		if err := rows.Scan(&sessionID, &rejectedFrames, &raw); err != nil {
			return nil, fmt.Errorf("scan rejection summary: %w", err)
		}
		var summary RejectionSummary
		if err := json.Unmarshal(raw, &summary); err != nil {
			return nil, fmt.Errorf("unmarshal rejection summary: %w", err)
		}
		recordsBySession[sessionID] = append(recordsBySession[sessionID], RejectionSummaryRecord{SessionID: sessionID, RejectedFrames: rejectedFrames, Summary: summary})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rejection summaries: %w", err)
	}

	seen := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		if sessionID == "" {
			continue
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		result[sessionID] = aggregateRejectionSummaries(recordsBySession[sessionID])
	}

	return result, nil
}

var _ RejectionSummaryStore = (*PostgresRejectionSummaryStore)(nil)
