package sessions

import (
	"context"
	"time"
)

type Repository interface {
	Create(ctx context.Context, session Session) (Session, error)
	FindByID(ctx context.Context, id string) (Session, error)
	End(ctx context.Context, id string, endedAt time.Time) (Session, error)
}
