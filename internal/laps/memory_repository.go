package laps

import (
	"context"
	"sync"
)

type MemoryRepository struct {
	mu   sync.RWMutex
	laps map[string]map[int]CompletedLap
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{laps: make(map[string]map[int]CompletedLap)}
}

func (r *MemoryRepository) UpsertCompleted(ctx context.Context, completed []CompletedLap) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || len(completed) == 0 {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, lap := range completed {
		lap = lap.withDefaults()
		if err := lap.Validate(); err != nil {
			return err
		}
		byLap, ok := r.laps[lap.SessionID]
		if !ok {
			byLap = make(map[int]CompletedLap)
			r.laps[lap.SessionID] = byLap
		}
		if _, exists := byLap[lap.LapNumber]; exists {
			continue
		}
		byLap[lap.LapNumber] = lap
	}

	return nil
}

func (r *MemoryRepository) ListBySession(ctx context.Context, sessionID string) ([]CompletedLap, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || sessionID == "" {
		return nil, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	byLap := r.laps[sessionID]
	result := make([]CompletedLap, 0, len(byLap))
	for _, lap := range byLap {
		result = append(result, lap.withDefaults())
	}
	sortByLapNumber(result)
	return cloneLaps(result), nil
}

var _ Repository = (*MemoryRepository)(nil)
