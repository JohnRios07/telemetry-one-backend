package sessions

import (
	"context"
	"time"
)

type Repository interface {
	Create(ctx context.Context, session Session) (Session, error)
	FindByID(ctx context.Context, id string) (Session, error)
	End(ctx context.Context, id string, endedAt time.Time) (Session, error)
	Update(ctx context.Context, session Session) (Session, error)
	List(ctx context.Context) ([]Session, error)
	SetDetectedTrackLayout(ctx context.Context, id string, trackID, layoutID string) (Session, error)
	SetTrackLayout(ctx context.Context, id string, trackID, layoutID string) (Session, error)
}

var _ Repository = (*MemoryRepository)(nil)
var _ Repository = (*PostgresRepository)(nil)
