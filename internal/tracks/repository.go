package tracks

import "context"

type Repository interface {
	Upsert(ctx context.Context, track Track) (Track, error)
	FindByID(ctx context.Context, id string) (Track, error)
	List(ctx context.Context) ([]Track, error)
}

type CornerRepository interface {
	ListByTrackID(ctx context.Context, trackID string) ([]Corner, error)
}
