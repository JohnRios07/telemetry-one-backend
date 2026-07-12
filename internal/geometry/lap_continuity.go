package geometry

import (
	"errors"
	"fmt"
	"math"
)

var ErrInvalidProjectionDistance = errors.New("projection distance must be finite")

type LapContinuityOptions struct {
	JitterToleranceMeters float64
	MaxForwardDeltaMeters float64
	MaxReverseDeltaMeters float64
}

type LapContinuityState struct {
	Initialized            bool
	Closed                 bool
	LapIndex               int
	LapCount               int
	DistanceMeters         float64
	Progress               float64
	ForwardDeltaMeters     float64
	ReverseDeltaMeters     float64
	TotalForwardMeters     float64
	WrappedStartFinishLine bool
	Reversed               bool
	LargeJump              bool
	Accepted               bool
}

type LapContinuityTracker struct {
	options LapContinuityOptions
	state   LapContinuityState
}

func DefaultLapContinuityOptions() LapContinuityOptions {
	return LapContinuityOptions{
		JitterToleranceMeters: 1,
		MaxForwardDeltaMeters: 250,
		MaxReverseDeltaMeters: 25,
	}
}

func NewLapContinuityTracker(options LapContinuityOptions) *LapContinuityTracker {
	return &LapContinuityTracker{options: normalizeLapContinuityOptions(options)}
}

func (t *LapContinuityTracker) Reset() {
	t.state = LapContinuityState{}
}

func (t *LapContinuityTracker) State() LapContinuityState {
	return t.state
}

func (t *LapContinuityTracker) Update(distanceMeters float64, totalMeters float64, closed bool) (LapContinuityState, error) {
	if totalMeters <= 0 {
		return t.state, ErrInvalidTrackLength
	}
	if !isFinite(distanceMeters) {
		return t.state, ErrInvalidProjectionDistance
	}

	currentDistance := clampOpenDistance(distanceMeters, totalMeters)
	if closed {
		currentDistance = normalizeDistance(distanceMeters, totalMeters)
	}
	currentProgress := normalizedProgress(currentDistance, totalMeters, closed)

	if !t.state.Initialized || t.state.Closed != closed {
		t.state = LapContinuityState{
			Initialized:    true,
			Closed:         closed,
			DistanceMeters: currentDistance,
			Progress:       currentProgress,
			Accepted:       true,
		}
		return t.state, nil
	}

	previous := t.state
	state := previous
	state.DistanceMeters = previous.DistanceMeters
	state.Progress = previous.Progress
	state.ForwardDeltaMeters = 0
	state.ReverseDeltaMeters = 0
	state.WrappedStartFinishLine = false
	state.Reversed = false
	state.LargeJump = false
	state.Accepted = false

	if closed {
		state = t.updateClosed(state, currentDistance, currentProgress, totalMeters)
	} else {
		state = t.updateOpen(state, currentDistance, currentProgress)
	}

	t.state = state
	return t.state, nil
}

func (t *LapContinuityTracker) updateClosed(state LapContinuityState, currentDistance float64, currentProgress float64, totalMeters float64) LapContinuityState {
	forwardDelta, _ := ForwardDelta(state.DistanceMeters, currentDistance, totalMeters)
	reverseDelta, _ := ForwardDelta(currentDistance, state.DistanceMeters, totalMeters)

	if forwardDelta <= t.options.JitterToleranceMeters || reverseDelta <= t.options.JitterToleranceMeters {
		state.Accepted = true
		return state
	}
	if forwardDelta <= t.options.MaxForwardDeltaMeters && forwardDelta < reverseDelta {
		state.ForwardDeltaMeters = forwardDelta
		state.TotalForwardMeters += forwardDelta
		if currentDistance < state.DistanceMeters {
			state.WrappedStartFinishLine = true
			state.LapIndex++
			state.LapCount++
		}
		state.DistanceMeters = currentDistance
		state.Progress = currentProgress
		state.Accepted = true
		return state
	}
	if reverseDelta <= t.options.MaxReverseDeltaMeters {
		state.ReverseDeltaMeters = reverseDelta
		state.Reversed = true
		state.DistanceMeters = currentDistance
		state.Progress = currentProgress
		state.Accepted = true
		return state
	}

	state.LargeJump = true
	return state
}

func (t *LapContinuityTracker) updateOpen(state LapContinuityState, currentDistance float64, currentProgress float64) LapContinuityState {
	delta := currentDistance - state.DistanceMeters
	if math.Abs(delta) <= t.options.JitterToleranceMeters {
		state.Accepted = true
		return state
	}
	if delta > 0 && delta <= t.options.MaxForwardDeltaMeters {
		state.ForwardDeltaMeters = delta
		state.TotalForwardMeters += delta
		state.DistanceMeters = currentDistance
		state.Progress = currentProgress
		state.Accepted = true
		return state
	}
	if delta < 0 && math.Abs(delta) <= t.options.MaxReverseDeltaMeters {
		state.ReverseDeltaMeters = math.Abs(delta)
		state.Reversed = true
		state.DistanceMeters = currentDistance
		state.Progress = currentProgress
		state.Accepted = true
		return state
	}

	state.LargeJump = true
	return state
}

func normalizeLapContinuityOptions(options LapContinuityOptions) LapContinuityOptions {
	defaults := DefaultLapContinuityOptions()
	if options.JitterToleranceMeters < 0 {
		options.JitterToleranceMeters = defaults.JitterToleranceMeters
	}
	if options.MaxForwardDeltaMeters <= 0 {
		options.MaxForwardDeltaMeters = defaults.MaxForwardDeltaMeters
	}
	if options.MaxReverseDeltaMeters <= 0 {
		options.MaxReverseDeltaMeters = defaults.MaxReverseDeltaMeters
	}

	return options
}

func clampOpenDistance(distanceMeters float64, totalMeters float64) float64 {
	if distanceMeters < 0 {
		return 0
	}
	if distanceMeters > totalMeters {
		return totalMeters
	}

	return distanceMeters
}

func UpdateLapContinuity(tracker *LapContinuityTracker, projection Projection, centerline Centerline) (LapContinuityState, error) {
	if tracker == nil {
		return LapContinuityState{}, fmt.Errorf("lap continuity tracker is required")
	}

	return tracker.Update(projection.DistanceFromStart, centerline.TotalMeters, centerline.Closed)
}
