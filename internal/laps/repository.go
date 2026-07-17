package laps

import "context"

type Repository interface {
	UpsertCompleted(ctx context.Context, completed []CompletedLap) error
	ListBySession(ctx context.Context, sessionID string) ([]CompletedLap, error)
}
