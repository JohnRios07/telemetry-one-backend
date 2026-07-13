package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresEventsDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PostgresRepository struct {
	mu           sync.Mutex
	pool         postgresEventsDB
	deduplicator *EventDeduplicator
}

func NewPostgresRepository(pool *pgxpool.Pool, dedupOptions DedupOptions) *PostgresRepository {
	var db postgresEventsDB
	if pool != nil {
		db = pool
	}
	return &PostgresRepository{pool: db, deduplicator: NewEventDeduplicator(dedupOptions)}
}

func (r *PostgresRepository) Append(ctx context.Context, event EngineerEvent) (EngineerEvent, bool, error) {
	if err := ctx.Err(); err != nil {
		return EngineerEvent{}, false, err
	}
	if err := event.Validate(); err != nil {
		return EngineerEvent{}, false, err
	}
	r.mu.Lock()
	accepted, _ := r.deduplicator.Filter([]EngineerEvent{event})
	r.mu.Unlock()
	if len(accepted) == 0 {
		return event, false, nil
	}

	fullEvent, err := json.Marshal(event)
	if err != nil {
		return EngineerEvent{}, false, fmt.Errorf("marshal event: %w", err)
	}
	metrics, err := json.Marshal(event.Metrics)
	if err != nil {
		return EngineerEvent{}, false, fmt.Errorf("marshal event metrics: %w", err)
	}
	metadata, err := json.Marshal(map[string]json.RawMessage{"event": fullEvent})
	if err != nil {
		return EngineerEvent{}, false, fmt.Errorf("marshal event metadata: %w", err)
	}

	commandTag, err := r.pool.Exec(ctx, `
INSERT INTO engineer_events (id, session_id, lap_number, corner_id, type, severity, timestamp_ms, metrics, metadata)
VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8::jsonb, $9::jsonb)
ON CONFLICT (id) DO NOTHING`,
		event.EventID,
		event.SessionID,
		event.LapNumber,
		catalogRefID(event.Corner),
		event.Type,
		event.Severity,
		event.TimestampUnixMs,
		metrics,
		metadata,
	)
	if err != nil {
		return EngineerEvent{}, false, fmt.Errorf("append event: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return event, false, nil
	}

	return event, true, nil
}

func (r *PostgresRepository) List(ctx context.Context, query Query) ([]EngineerEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := query.Validate(); err != nil {
		return nil, err
	}

	var builder strings.Builder
	builder.WriteString(`SELECT metadata->'event' FROM engineer_events WHERE session_id = $1`)
	args := []any{query.SessionID}
	if query.LapNumber != nil {
		args = append(args, *query.LapNumber)
		builder.WriteString(fmt.Sprintf(" AND lap_number = $%d", len(args)))
	}
	if query.CornerID != "" {
		args = append(args, query.CornerID)
		builder.WriteString(fmt.Sprintf(" AND corner_id = $%d", len(args)))
	}
	if query.Type != "" {
		args = append(args, query.Type)
		builder.WriteString(fmt.Sprintf(" AND type = $%d", len(args)))
	}
	builder.WriteString(" ORDER BY timestamp_ms ASC, id ASC")

	rows, err := r.pool.Query(ctx, builder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	result := make([]EngineerEvent, 0)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}

	return result, nil
}

func scanEvent(row pgx.Row) (EngineerEvent, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return EngineerEvent{}, fmt.Errorf("scan event: %w", err)
	}
	var event EngineerEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return EngineerEvent{}, fmt.Errorf("unmarshal event: %w", err)
	}

	return event, nil
}

var _ Repository = (*PostgresRepository)(nil)
