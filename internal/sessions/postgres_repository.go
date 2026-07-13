package sessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Create(ctx context.Context, session Session) (Session, error) {
	if session.ID == "" {
		return Session{}, ErrMissingID
	}

	row := r.pool.QueryRow(ctx, `
INSERT INTO sessions (id, source, game, platform, driver_alias, track_id, started_at, ended_at)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8)
RETURNING id, source, game, platform, driver_alias, COALESCE(track_id, ''), started_at, ended_at`,
		session.ID,
		session.Source,
		session.Game,
		session.Platform,
		session.DriverAlias,
		session.TrackID,
		session.StartedAt.UTC(),
		session.EndedAt,
	)

	created, err := scanSession(row)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}

	return created, nil
}

func (r *PostgresRepository) FindByID(ctx context.Context, id string) (Session, error) {
	session, err := scanSession(r.pool.QueryRow(ctx, selectSessionSQL+" WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("find session: %w", err)
	}

	return session, nil
}

func (r *PostgresRepository) End(ctx context.Context, id string, endedAt time.Time) (Session, error) {
	endedAt = endedAt.UTC()

	session, err := scanSession(r.pool.QueryRow(ctx, `
UPDATE sessions
SET ended_at = $2, updated_at = now()
WHERE id = $1 AND ended_at IS NULL AND started_at <= $2
RETURNING id, source, game, platform, driver_alias, COALESCE(track_id, ''), started_at, ended_at`, id, endedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, r.classifyEndNoRows(ctx, id, endedAt)
	}
	if err != nil {
		return Session{}, fmt.Errorf("end session: %w", err)
	}

	return session, nil
}

func (r *PostgresRepository) classifyEndNoRows(ctx context.Context, id string, endedAt time.Time) error {
	current, err := r.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if current.EndedAt != nil {
		return ErrAlreadyFinished
	}
	if endedAt.Before(current.StartedAt) {
		return ErrInvalidEndedAt
	}

	return fmt.Errorf("end session: active session %q was not updated", id)
}

func (r *PostgresRepository) Update(ctx context.Context, session Session) (Session, error) {
	if session.ID == "" {
		return Session{}, ErrMissingID
	}

	updated, err := scanSession(r.pool.QueryRow(ctx, `
UPDATE sessions
SET source = $2, game = $3, platform = $4, driver_alias = $5, track_id = NULLIF($6, ''), started_at = $7, ended_at = $8, updated_at = now()
WHERE id = $1
RETURNING id, source, game, platform, driver_alias, COALESCE(track_id, ''), started_at, ended_at`,
		session.ID,
		session.Source,
		session.Game,
		session.Platform,
		session.DriverAlias,
		session.TrackID,
		session.StartedAt.UTC(),
		session.EndedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("update session: %w", err)
	}

	return updated, nil
}

func (r *PostgresRepository) List(ctx context.Context) ([]Session, error) {
	rows, err := r.pool.Query(ctx, selectSessionSQL+" ORDER BY started_at DESC, id DESC")
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var result []Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		result = append(result, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}

	return result, nil
}

const selectSessionSQL = `SELECT id, source, game, platform, driver_alias, COALESCE(track_id, ''), started_at, ended_at FROM sessions`

type sessionScanner interface {
	Scan(dest ...any) error
}

func scanSession(scanner sessionScanner) (Session, error) {
	var session Session
	if err := scanner.Scan(
		&session.ID,
		&session.Source,
		&session.Game,
		&session.Platform,
		&session.DriverAlias,
		&session.TrackID,
		&session.StartedAt,
		&session.EndedAt,
	); err != nil {
		return Session{}, err
	}
	session.StartedAt = session.StartedAt.UTC()
	if session.EndedAt != nil {
		endedAt := session.EndedAt.UTC()
		session.EndedAt = &endedAt
	}

	return session, nil
}
