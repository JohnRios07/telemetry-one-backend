package laps

import (
	"errors"
	"fmt"
	"sort"

	"telemetry-one-backend/internal/telemetry"
)

var (
	ErrMissingSessionID = errors.New("sessionId is required")
	ErrInvalidLapNumber = errors.New("lapNumber must be zero or greater")
	ErrInvalidLapTime   = errors.New("lapTimeMs must be greater than zero")
	ErrInvalidTimestamp = errors.New("completedAtUnixMs must be greater than zero")
)

type CompletedLap struct {
	ID                string
	SessionID         string
	LapNumber         int
	LapTimeMs         int64
	CompletedAtUnixMs int64
	BestLapMs         *int64
	SampleCount       *int
}

func NewCompletedLapID(sessionID string, lapNumber int) string {
	return fmt.Sprintf("lap_%s_%d", sessionID, lapNumber)
}

func (lap CompletedLap) Validate() error {
	if lap.SessionID == "" {
		return ErrMissingSessionID
	}
	if lap.LapNumber < 0 {
		return ErrInvalidLapNumber
	}
	if lap.LapTimeMs <= 0 {
		return ErrInvalidLapTime
	}
	if lap.CompletedAtUnixMs <= 0 {
		return ErrInvalidTimestamp
	}
	if lap.BestLapMs != nil && *lap.BestLapMs <= 0 {
		return ErrInvalidLapTime
	}
	if lap.SampleCount != nil && *lap.SampleCount <= 0 {
		return errors.New("sampleCount must be greater than zero")
	}
	return nil
}

func (lap CompletedLap) withDefaults() CompletedLap {
	if lap.ID == "" && lap.SessionID != "" && lap.LapNumber >= 0 {
		lap.ID = NewCompletedLapID(lap.SessionID, lap.LapNumber)
	}
	lap.BestLapMs = cloneInt64(lap.BestLapMs)
	lap.SampleCount = cloneInt(lap.SampleCount)
	return lap
}

func ExtractCompleted(sessionID string, frames []telemetry.Frame) []CompletedLap {
	if sessionID == "" || len(frames) == 0 {
		return nil
	}

	seen := make(map[int]struct{}, len(frames))
	completed := make([]CompletedLap, 0, len(frames))
	for _, frame := range frames {
		if frame.LapNumber <= 0 || frame.LastLapMs == nil || *frame.LastLapMs <= 0 || frame.TimestampUnixMs <= 0 {
			continue
		}

		completedLapNumber := frame.LapNumber - 1
		if completedLapNumber < 0 {
			continue
		}
		if _, exists := seen[completedLapNumber]; exists {
			continue
		}

		seen[completedLapNumber] = struct{}{}
		completed = append(completed, CompletedLap{
			ID:                NewCompletedLapID(sessionID, completedLapNumber),
			SessionID:         sessionID,
			LapNumber:         completedLapNumber,
			LapTimeMs:         *frame.LastLapMs,
			CompletedAtUnixMs: frame.TimestampUnixMs,
			BestLapMs:         cloneInt64(frame.BestLapMs),
		})
	}

	return completed
}

func cloneLaps(input []CompletedLap) []CompletedLap {
	cloned := make([]CompletedLap, len(input))
	for i, lap := range input {
		cloned[i] = lap.withDefaults()
	}
	return cloned
}

func sortByLapNumber(laps []CompletedLap) {
	sort.Slice(laps, func(i, j int) bool {
		if laps[i].LapNumber == laps[j].LapNumber {
			return laps[i].ID < laps[j].ID
		}
		return laps[i].LapNumber < laps[j].LapNumber
	})
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
