package sessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresSummaryRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresSummaryRepository(pool *pgxpool.Pool) *PostgresSummaryRepository {
	return &PostgresSummaryRepository{pool: pool}
}

func (r *PostgresSummaryRepository) List(ctx context.Context, filter SummaryFilter) (*ListResponse, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}

	query := `
SELECT
	s.id, s.source, s.game, s.platform, s.driver_alias, COALESCE(s.track_id, ''),
	s.started_at, s.ended_at,
	COALESCE(fb.batches, 0),
	COALESCE(fb.persisted, 0),
	COALESCE(ee.event_count, 0),
	COALESCE(s.detected_track_id, ''), COALESCE(s.detected_layout_id, '')
FROM sessions s
LEFT JOIN LATERAL (
	SELECT
		COUNT(*) AS batches,
		COALESCE(SUM(frame_count), 0) AS persisted
	FROM frame_batches
	WHERE session_id = s.id
) fb ON true
LEFT JOIN LATERAL (
	SELECT COUNT(*) AS event_count
	FROM engineer_events
	WHERE session_id = s.id
) ee ON true
ORDER BY s.started_at DESC, s.id DESC
LIMIT $1`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list session summaries: %w", err)
	}
	defer rows.Close()

	items := make([]SessionSummaryItem, 0)
	for rows.Next() {
		item, err := scanSummaryItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session summaries: %w", err)
	}

	return &ListResponse{Sessions: items}, nil
}

func (r *PostgresSummaryRepository) Summary(ctx context.Context, sessionID string) (*SessionDetailSummary, error) {
	if sessionID == "" {
		return nil, ErrMissingID
	}

	query := `
SELECT
	s.id, s.source, s.game, s.platform, s.driver_alias, COALESCE(s.track_id, ''),
	s.started_at, s.ended_at,
	COALESCE(fb.batches, 0),
	COALESCE(fb.persisted, 0),
	COALESCE(fb.time_start_ms, 0),
	COALESCE(fb.time_end_ms, 0),
	COALESCE(fb.laps, 0),
	COALESCE(ee.event_count, 0),
	COALESCE(al.audit_count, 0),
	COALESCE(s.detected_track_id, ''), COALESCE(s.detected_layout_id, '')
FROM sessions s
LEFT JOIN LATERAL (
	SELECT
		COUNT(*) AS batches,
		COALESCE(SUM(frame_count), 0) AS persisted,
		COUNT(DISTINCT lap_number) AS laps,
		MIN(time_start_ms) AS time_start_ms,
		MAX(time_end_ms) AS time_end_ms
	FROM frame_batches
	WHERE session_id = s.id
) fb ON true
LEFT JOIN LATERAL (
	SELECT COUNT(*) AS event_count
	FROM engineer_events
	WHERE session_id = s.id
) ee ON true
LEFT JOIN LATERAL (
	SELECT COUNT(*) AS audit_count
	FROM ai_audit_logs
	WHERE session_id = s.id
) al ON true
WHERE s.id = $1`

	var item SessionSummaryItem
	var startedAt time.Time
	var endedAt *time.Time
	var fb struct {
		batches  int
		persisted int
		timeStart int64
		timeEnd  int64
		laps     int
	}
	var eeCount, alCount int

	var detectedTrackID, detectedLayoutID string
	err := r.pool.QueryRow(ctx, query, sessionID).Scan(
		&item.ID, &item.Source, &item.Game, &item.Platform,
		&item.DriverAlias, &item.TrackID,
		&startedAt, &endedAt,
		&fb.batches, &fb.persisted,
		&fb.timeStart, &fb.timeEnd,
		&fb.laps,
		&eeCount, &alCount,
		&detectedTrackID, &detectedLayoutID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query session summary: %w", err)
	}

	item.StartedAt = startedAt.UTC().Format(time.RFC3339)
	item.Status = StatusActive
	if endedAt != nil {
		e := endedAt.UTC().Format(time.RFC3339)
		item.EndedAt = &e
		d := endedAt.Sub(startedAt).Milliseconds()
		item.DurationMs = &d
		item.Status = StatusFinished
	}

	item.FrameBatches = fb.batches
	item.PersistedFrames = fb.persisted
	item.EventCount = eeCount

	if detectedTrackID != "" {
		item.DetectedTrackID = &detectedTrackID
	}
	if detectedLayoutID != "" {
		item.DetectedLayoutID = &detectedLayoutID
	}

	summary := &SessionDetailSummary{
		Session:            item,
		FrameBatches:       fb.batches,
		PersistedFrames:    fb.persisted,
		LapsDetected:       fb.laps,
		EngineerEventCount: eeCount,
		AIAuditLogCount:    alCount,
	}

	if fb.batches > 0 {
		summary.TimeRangeMs = &TimeRange{From: fb.timeStart, To: fb.timeEnd}
	}

	return summary, nil
}

type summaryItemScanner interface {
	Scan(dest ...any) error
}

func scanSummaryItem(scanner summaryItemScanner) (SessionSummaryItem, error) {
	var item SessionSummaryItem
	var startedAt time.Time
	var endedAt *time.Time
	var frameBatches, persistedFrames, eventCount int
	var detectedTrackID, detectedLayoutID string

	if err := scanner.Scan(
		&item.ID, &item.Source, &item.Game, &item.Platform,
		&item.DriverAlias, &item.TrackID,
		&startedAt, &endedAt,
		&frameBatches, &persistedFrames,
		&eventCount,
		&detectedTrackID, &detectedLayoutID,
	); err != nil {
		return SessionSummaryItem{}, err
	}

	item.StartedAt = startedAt.UTC().Format(time.RFC3339)
	item.FrameBatches = frameBatches
	item.PersistedFrames = persistedFrames
	item.EventCount = eventCount
	item.Status = StatusActive
	if endedAt != nil {
		e := endedAt.UTC().Format(time.RFC3339)
		item.EndedAt = &e
		d := endedAt.Sub(startedAt).Milliseconds()
		item.DurationMs = &d
		item.Status = StatusFinished
	}

	if detectedTrackID != "" {
		item.DetectedTrackID = &detectedTrackID
	}
	if detectedLayoutID != "" {
		item.DetectedLayoutID = &detectedLayoutID
	}

	return item, nil
}
