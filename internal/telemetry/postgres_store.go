package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresFrameDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PostgresStore struct {
	pool    postgresFrameDB
	timeout time.Duration
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	var db postgresFrameDB
	if pool != nil {
		db = pool
	}
	return &PostgresStore{pool: db, timeout: 5 * time.Second}
}

func (s *PostgresStore) Append(ctx context.Context, sessionID string, frames []Frame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if sessionID == "" || len(frames) == 0 || s == nil || s.pool == nil {
		return nil
	}

	incoming := cloneFrames(frames)
	for _, batch := range splitFramesByLap(incoming) {
		if err := s.appendBatch(ctx, sessionID, batch); err != nil {
			return err
		}
	}

	return nil
}

func (s *PostgresStore) appendBatch(ctx context.Context, sessionID string, frames []Frame) error {
	body, err := json.Marshal(frames)
	if err != nil {
		return fmt.Errorf("marshal frame batch: %w", err)
	}

	fromUnixMs, toUnixMs := frameTimeRange(frames)
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	_, err = s.pool.Exec(ctx, `
WITH locked AS (
    SELECT pg_advisory_xact_lock(hashtextextended($1, 0))
), next_batch AS (
    SELECT COALESCE(MAX(batch_index), -1) + 1 AS batch_index
    FROM frame_batches
    WHERE session_id = $1
)
INSERT INTO frame_batches (id, session_id, lap_number, batch_index, frame_count, frames_jsonb, time_start_ms, time_end_ms)
SELECT $2, $1, $3, batch_index, $4, $5::jsonb, $6, $7
FROM next_batch, locked`, sessionID, newFrameBatchID(), frames[0].LapNumber, len(frames), body, fromUnixMs, toUnixMs)
	if err != nil {
		return fmt.Errorf("append frame batch: %w", err)
	}

	return nil
}

func (s *PostgresStore) Frames(ctx context.Context, sessionID string) ([]Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if sessionID == "" || s == nil || s.pool == nil {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT frames_jsonb FROM frame_batches WHERE session_id = $1 ORDER BY batch_index ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query frame batches: %w", err)
	}
	defer rows.Close()

	var result []Frame
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan frame batch: %w", err)
		}
		var batch []Frame
		if err := json.Unmarshal(raw, &batch); err != nil {
			return nil, fmt.Errorf("unmarshal frame batch: %w", err)
		}
		result = append(result, batch...)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate frame batches: %w", err)
	}

	return cloneFrames(result), nil
}

func splitFramesByLap(frames []Frame) [][]Frame {
	if len(frames) == 0 {
		return nil
	}

	batches := make([][]Frame, 0, 1)
	start := 0
	for index := 1; index < len(frames); index++ {
		if frames[index].LapNumber == frames[start].LapNumber {
			continue
		}
		batches = append(batches, frames[start:index])
		start = index
	}
	batches = append(batches, frames[start:])

	return batches
}

func frameTimeRange(frames []Frame) (int64, int64) {
	fromUnixMs := frames[0].TimestampUnixMs
	toUnixMs := frames[0].TimestampUnixMs
	for _, frame := range frames[1:] {
		if frame.TimestampUnixMs < fromUnixMs {
			fromUnixMs = frame.TimestampUnixMs
		}
		if frame.TimestampUnixMs > toUnixMs {
			toUnixMs = frame.TimestampUnixMs
		}
	}

	return fromUnixMs, toUnixMs
}

func newFrameBatchID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("frame_batch_%d", time.Now().UnixNano())
	}
	return "frame_batch_" + hex.EncodeToString(bytes[:])
}

var _ Store = (*FrameStore)(nil)
var _ Store = (*PostgresStore)(nil)
