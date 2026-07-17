package laps

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresLapsDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PostgresRepository struct {
	pool postgresLapsDB
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	var db postgresLapsDB
	if pool != nil {
		db = pool
	}
	return &PostgresRepository{pool: db}
}

func (r *PostgresRepository) UpsertCompleted(ctx context.Context, completed []CompletedLap) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.pool == nil || len(completed) == 0 {
		return nil
	}

	for _, lap := range completed {
		lap = lap.withDefaults()
		if err := lap.Validate(); err != nil {
			return err
		}
		if _, err := r.pool.Exec(ctx, `
INSERT INTO laps (id, session_id, lap_number, lap_time_ms, completed_at_unix_ms, best_lap_ms, sample_count)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (session_id, lap_number) DO UPDATE SET
    updated_at = laps.updated_at
WHERE laps.lap_time_ms = EXCLUDED.lap_time_ms
  AND laps.completed_at_unix_ms = EXCLUDED.completed_at_unix_ms`,
			lap.ID,
			lap.SessionID,
			lap.LapNumber,
			lap.LapTimeMs,
			lap.CompletedAtUnixMs,
			lap.BestLapMs,
			lap.SampleCount,
		); err != nil {
			return fmt.Errorf("upsert completed lap: %w", err)
		}
	}

	return nil
}

func (r *PostgresRepository) ListBySession(ctx context.Context, sessionID string) ([]CompletedLap, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.pool == nil || sessionID == "" {
		return nil, nil
	}

	rows, err := r.pool.Query(ctx, `
SELECT id, session_id, lap_number, lap_time_ms, completed_at_unix_ms, best_lap_ms, sample_count
FROM laps
WHERE session_id = $1
ORDER BY lap_number ASC, id ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list completed laps: %w", err)
	}
	defer rows.Close()

	result := make([]CompletedLap, 0)
	for rows.Next() {
		var lap CompletedLap
		if err := rows.Scan(&lap.ID, &lap.SessionID, &lap.LapNumber, &lap.LapTimeMs, &lap.CompletedAtUnixMs, &lap.BestLapMs, &lap.SampleCount); err != nil {
			return nil, fmt.Errorf("scan completed lap: %w", err)
		}
		result = append(result, lap.withDefaults())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate completed laps: %w", err)
	}

	return cloneLaps(result), nil
}

var _ Repository = (*PostgresRepository)(nil)
