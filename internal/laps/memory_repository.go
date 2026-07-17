package laps

import (
	"context"
	"sync"
)

type MemoryRepository struct {
	mu      sync.RWMutex
	laps    map[string]map[int]CompletedLap
	samples map[string]map[int]LapSample
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		laps:    make(map[string]map[int]CompletedLap),
		samples: make(map[string]map[int]LapSample),
	}
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
		if existing, exists := byLap[lap.LapNumber]; exists {
			existing.TelemetryGapCount = lap.TelemetryGapCount
			byLap[lap.LapNumber] = existing
			continue
		}
		byLap[lap.LapNumber] = lap
	}

	return nil
}

func (r *MemoryRepository) UpdateTelemetryGapCount(ctx context.Context, sessionID string, lapNumber int, count int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || sessionID == "" {
		return nil
	}
	if lapNumber < 0 {
		return ErrInvalidLapNumber
	}
	if count < 0 {
		return ErrInvalidGapCount
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	byLap := r.laps[sessionID]
	if byLap == nil {
		return nil
	}
	lap, ok := byLap[lapNumber]
	if !ok {
		return nil
	}
	lap.TelemetryGapCount = count
	byLap[lapNumber] = lap
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

func (r *MemoryRepository) UpsertSamples(ctx context.Context, samples []LapSample) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || len(samples) == 0 {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, sample := range samples {
		sample = sample.withDefaults()
		if err := sample.Validate(); err != nil {
			return err
		}
		byDistance, ok := r.samples[sample.LapID]
		if !ok {
			byDistance = make(map[int]LapSample)
			r.samples[sample.LapID] = byDistance
		}
		if _, exists := byDistance[sample.DistanceMeters]; exists {
			continue
		}
		byDistance[sample.DistanceMeters] = sample
	}

	return nil
}

func (r *MemoryRepository) ListSamples(ctx context.Context, lapID string) ([]LapSample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || lapID == "" {
		return nil, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	byDistance := r.samples[lapID]
	result := make([]LapSample, 0, len(byDistance))
	for _, sample := range byDistance {
		result = append(result, sample.withDefaults())
	}
	sortByDistance(result)
	return cloneSamples(result), nil
}

var _ Repository = (*MemoryRepository)(nil)
var _ SampleRepository = (*MemoryRepository)(nil)
