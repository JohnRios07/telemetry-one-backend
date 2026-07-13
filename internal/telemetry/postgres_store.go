package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool, timeout: 5 * time.Second}
}

func (s *PostgresStore) Append(sessionID string, frames []Frame) {
	if sessionID == "" || len(frames) == 0 || s == nil || s.pool == nil {
		return
	}

	incoming := cloneFrames(frames)
	body, err := json.Marshal(incoming)
	if err != nil {
		return
	}

	fromUnixMs, toUnixMs := frameTimeRange(incoming)
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	_, _ = s.pool.Exec(ctx, `
WITH locked AS (
    SELECT pg_advisory_xact_lock(hashtext($1))
), next_batch AS (
    SELECT COALESCE(MAX(batch_index), -1) + 1 AS batch_index
    FROM frame_batches
    WHERE session_id = $1
)
INSERT INTO frame_batches (id, session_id, lap_number, batch_index, frame_count, frames_jsonb, time_start_ms, time_end_ms)
SELECT $2, $1, $3, batch_index, $4, $5::jsonb, $6, $7
FROM next_batch, locked`, sessionID, newFrameBatchID(), incoming[0].LapNumber, len(incoming), body, fromUnixMs, toUnixMs)
}

func (s *PostgresStore) Frames(sessionID string) []Frame {
	if sessionID == "" || s == nil || s.pool == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT frames_jsonb FROM frame_batches WHERE session_id = $1 ORDER BY batch_index ASC`, sessionID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []Frame
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil
		}
		var batch []Frame
		if err := json.Unmarshal(raw, &batch); err != nil {
			return nil
		}
		result = append(result, batch...)
	}
	if rows.Err() != nil {
		return nil
	}

	return cloneFrames(result)
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
