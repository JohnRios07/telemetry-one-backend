package laps

import "context"

type Repository interface {
	UpsertCompleted(ctx context.Context, completed []CompletedLap) error
	ListBySession(ctx context.Context, sessionID string) ([]CompletedLap, error)
}

type SampleRepository interface {
	UpsertSamples(ctx context.Context, samples []LapSample) error
	ListSamples(ctx context.Context, lapID string) ([]LapSample, error)
}
