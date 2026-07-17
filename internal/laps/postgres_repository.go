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
INSERT INTO laps (id, session_id, lap_number, lap_time_ms, completed_at_unix_ms, best_lap_ms, sample_count, telemetry_gap_count)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (session_id, lap_number) DO UPDATE SET
    telemetry_gap_count = EXCLUDED.telemetry_gap_count,
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
			lap.TelemetryGapCount,
		); err != nil {
			return fmt.Errorf("upsert completed lap: %w", err)
		}
	}

	return nil
}

func (r *PostgresRepository) UpdateTelemetryGapCount(ctx context.Context, sessionID string, lapNumber int, count int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.pool == nil || sessionID == "" {
		return nil
	}
	if lapNumber < 0 {
		return ErrInvalidLapNumber
	}
	if count < 0 {
		return ErrInvalidGapCount
	}

	if _, err := r.pool.Exec(ctx, `
UPDATE laps
SET telemetry_gap_count = $3,
    updated_at = NOW()
WHERE session_id = $1
  AND lap_number = $2`, sessionID, lapNumber, count); err != nil {
		return fmt.Errorf("update telemetry gap count: %w", err)
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
SELECT id, session_id, lap_number, lap_time_ms, completed_at_unix_ms, best_lap_ms, sample_count, telemetry_gap_count
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
		if err := rows.Scan(&lap.ID, &lap.SessionID, &lap.LapNumber, &lap.LapTimeMs, &lap.CompletedAtUnixMs, &lap.BestLapMs, &lap.SampleCount, &lap.TelemetryGapCount); err != nil {
			return nil, fmt.Errorf("scan completed lap: %w", err)
		}
		result = append(result, lap.withDefaults())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate completed laps: %w", err)
	}

	return cloneLaps(result), nil
}

func (r *PostgresRepository) UpsertSamples(ctx context.Context, samples []LapSample) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.pool == nil || len(samples) == 0 {
		return nil
	}

	for _, sample := range samples {
		sample = sample.withDefaults()
		if err := sample.Validate(); err != nil {
			return err
		}
		if _, err := r.pool.Exec(ctx, `
INSERT INTO lap_samples (
    id, lap_id, distance_meters, source, timestamp_unix_ms,
    speed_mps, rpm, gear, throttle, brake, steering, fuel_liters,
    yaw_radians, yaw_rate, wheel_speed_fl, wheel_speed_fr, wheel_speed_rl, wheel_speed_rr
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
ON CONFLICT (lap_id, distance_meters) DO NOTHING`,
			sample.ID,
			sample.LapID,
			sample.DistanceMeters,
			sample.Source,
			sample.TimestampUnixMs,
			sample.SpeedMps,
			sample.RPM,
			sample.Gear,
			sample.Throttle,
			sample.Brake,
			sample.Steering,
			sample.FuelLiters,
			sample.YawRadians,
			sample.YawRate,
			sample.WheelSpeedFL,
			sample.WheelSpeedFR,
			sample.WheelSpeedRL,
			sample.WheelSpeedRR,
		); err != nil {
			return fmt.Errorf("upsert lap sample: %w", err)
		}
	}

	return nil
}

func (r *PostgresRepository) ListSamples(ctx context.Context, lapID string) ([]LapSample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.pool == nil || lapID == "" {
		return nil, nil
	}

	rows, err := r.pool.Query(ctx, `
SELECT id, lap_id, distance_meters, source, timestamp_unix_ms,
       speed_mps, rpm, gear, throttle, brake, steering, fuel_liters,
       yaw_radians, yaw_rate, wheel_speed_fl, wheel_speed_fr, wheel_speed_rl, wheel_speed_rr
FROM lap_samples
WHERE lap_id = $1
ORDER BY distance_meters ASC, id ASC`, lapID)
	if err != nil {
		return nil, fmt.Errorf("list lap samples: %w", err)
	}
	defer rows.Close()

	result := make([]LapSample, 0)
	for rows.Next() {
		var sample LapSample
		if err := rows.Scan(
			&sample.ID,
			&sample.LapID,
			&sample.DistanceMeters,
			&sample.Source,
			&sample.TimestampUnixMs,
			&sample.SpeedMps,
			&sample.RPM,
			&sample.Gear,
			&sample.Throttle,
			&sample.Brake,
			&sample.Steering,
			&sample.FuelLiters,
			&sample.YawRadians,
			&sample.YawRate,
			&sample.WheelSpeedFL,
			&sample.WheelSpeedFR,
			&sample.WheelSpeedRL,
			&sample.WheelSpeedRR,
		); err != nil {
			return nil, fmt.Errorf("scan lap sample: %w", err)
		}
		result = append(result, sample.withDefaults())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lap samples: %w", err)
	}

	return cloneSamples(result), nil
}

var _ Repository = (*PostgresRepository)(nil)
var _ SampleRepository = (*PostgresRepository)(nil)
