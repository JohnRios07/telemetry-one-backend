package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStatsRepo struct {
	pool *pgxpool.Pool
}

func NewPostgresStatsRepo(pool *pgxpool.Pool) *PostgresStatsRepo {
	return &PostgresStatsRepo{pool: pool}
}

func (r *PostgresStatsRepo) Stats(ctx context.Context, limit int, days int) (*IngestStatsResponse, error) {
	totals, err := r.loadTotals(ctx)
	if err != nil {
		return nil, err
	}

	recent, err := r.loadRecentSessions(ctx, limit)
	if err != nil {
		return nil, err
	}

	daily, err := r.loadDailyStats(ctx, days)
	if err != nil {
		return nil, err
	}

	return &IngestStatsResponse{
		Mode:           ModePostgres,
		Totals:         *totals,
		RecentSessions: recent,
		Daily:          daily,
	}, nil
}

func (r *PostgresStatsRepo) loadTotals(ctx context.Context) (*Totals, error) {
	var totalSessions, activeSessions, finishedSessions int
	var frameBatches, persistedFrames, engineerEvents, aiAuditLogs int

	err := r.pool.QueryRow(ctx, `SELECT
		COALESCE((SELECT COUNT(*) FROM sessions), 0),
		COALESCE((SELECT COUNT(*) FROM sessions WHERE ended_at IS NULL), 0),
		COALESCE((SELECT COUNT(*) FROM sessions WHERE ended_at IS NOT NULL), 0),
		COALESCE((SELECT COUNT(*) FROM frame_batches), 0),
		COALESCE((SELECT SUM(frame_count) FROM frame_batches), 0),
		COALESCE((SELECT COUNT(*) FROM engineer_events), 0),
		COALESCE((SELECT COUNT(*) FROM ai_audit_logs), 0)`).Scan(
		&totalSessions, &activeSessions, &finishedSessions,
		&frameBatches, &persistedFrames, &engineerEvents, &aiAuditLogs,
	)
	if err != nil {
		return nil, fmt.Errorf("load totals: %w", err)
	}

	fb := frameBatches
	pf := persistedFrames
	ee := engineerEvents
	al := aiAuditLogs

	return &Totals{
		Sessions:         totalSessions,
		ActiveSessions:   activeSessions,
		FinishedSessions: finishedSessions,
		FrameBatches:     &fb,
		PersistedFrames:  &pf,
		EngineerEvents:   &ee,
		AIAuditLogs:      &al,
	}, nil
}

func (r *PostgresStatsRepo) loadRecentSessions(ctx context.Context, limit int) ([]RecentSession, error) {
	rows, err := r.pool.Query(ctx, `
SELECT
	s.id,
	s.started_at,
	s.ended_at,
	COALESCE(fb.batches, 0),
	COALESCE(fb.persisted, 0)
FROM sessions s
LEFT JOIN LATERAL (
	SELECT
		COUNT(*) AS batches,
		COALESCE(SUM(frame_count), 0) AS persisted
	FROM frame_batches
	WHERE session_id = s.id
) fb ON true
ORDER BY s.started_at DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("load recent sessions: %w", err)
	}
	defer rows.Close()

	result := make([]RecentSession, 0, limit)
	for rows.Next() {
		rs := RecentSession{}
		var startedAt time.Time
		var endedAt *time.Time
		if err := rows.Scan(&rs.ID, &startedAt, &endedAt, &rs.FrameBatches, &rs.PersistedFrames); err != nil {
			return nil, fmt.Errorf("scan recent session: %w", err)
		}
		rs.StartedAt = startedAt.UTC().Format(time.RFC3339)
		rs.Status = "active"
		if endedAt != nil {
			e := endedAt.UTC().Format(time.RFC3339)
			rs.EndedAt = &e
			d := endedAt.Sub(startedAt).Milliseconds()
			rs.DurationMs = &d
			rs.Status = "finished"
		}
		result = append(result, rs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent sessions: %w", err)
	}

	return result, nil
}

type dailyRow struct {
	Date            time.Time
	Sessions        int
	FrameBatches    int
	PersistedFrames int
}

func (r *PostgresStatsRepo) loadDailyStats(ctx context.Context, days int) ([]DailyStats, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	rows, err := r.pool.Query(ctx, `
SELECT
	DATE(s.started_at) AS date,
	COUNT(DISTINCT s.id) AS sessions,
	COALESCE(COUNT(fb.id), 0) AS frame_batches,
	COALESCE(SUM(fb.frame_count), 0) AS persisted_frames
FROM sessions s
LEFT JOIN frame_batches fb ON fb.session_id = s.id
WHERE s.started_at >= $1
GROUP BY DATE(s.started_at)
ORDER BY date ASC`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("load daily stats: %w", err)
	}
	defer rows.Close()

	result := make([]DailyStats, 0)
	for rows.Next() {
		var row dailyRow
		if err := rows.Scan(&row.Date, &row.Sessions, &row.FrameBatches, &row.PersistedFrames); err != nil {
			return nil, fmt.Errorf("scan daily stat: %w", err)
		}
		result = append(result, DailyStats{
			Date:            row.Date.Format("2006-01-02"),
			Sessions:        row.Sessions,
			FrameBatches:    row.FrameBatches,
			PersistedFrames: row.PersistedFrames,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily stats: %w", err)
	}

	return result, nil
}

var _ StatsRepository = (*PostgresStatsRepo)(nil)
var _ StatsRepository = (*MemoryStatsRepo)(nil)
