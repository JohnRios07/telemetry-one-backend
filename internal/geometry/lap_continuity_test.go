package geometry

import "testing"

func TestLapContinuityTracksForwardProgress(t *testing.T) {
	tracker := newTestLapTracker()

	updates := updateDistances(t, tracker, 100, true, 10, 25, 60)
	last := updates[len(updates)-1]

	assertNear(t, last.ForwardDeltaMeters, 35)
	assertNear(t, last.TotalForwardMeters, 50)
	assertNear(t, last.Progress, 0.6)
	if last.LapIndex != 0 || last.LapCount != 0 || last.WrappedStartFinishLine || last.Reversed || last.LargeJump || !last.Accepted {
		t.Fatalf("unexpected state: %+v", last)
	}
}

func TestLapContinuityDetectsStartFinishWrapAndLapIncrement(t *testing.T) {
	tracker := newTestLapTracker()

	updates := updateDistances(t, tracker, 100, true, 95, 99, 3)
	last := updates[len(updates)-1]

	assertNear(t, last.ForwardDeltaMeters, 4)
	assertNear(t, last.TotalForwardMeters, 8)
	if !last.WrappedStartFinishLine || last.LapIndex != 1 || last.LapCount != 1 || last.Reversed || last.LargeJump || !last.Accepted {
		t.Fatalf("unexpected wrap state: %+v", last)
	}
}

func TestLapContinuityIgnoresSmallJitterNearStartFinish(t *testing.T) {
	tracker := newTestLapTracker()

	updates := updateDistances(t, tracker, 100, true, 99.6, 0.3, 99.9, 0.2)
	last := updates[len(updates)-1]

	if last.LapCount != 0 || last.WrappedStartFinishLine || last.Reversed || last.LargeJump || !last.Accepted {
		t.Fatalf("unexpected jitter state: %+v", last)
	}
	assertNear(t, last.TotalForwardMeters, 0)
	assertNear(t, last.DistanceMeters, 99.6)
}

func TestLapContinuityCountsSlowWrapAfterStartFinishJitter(t *testing.T) {
	tracker := newTestLapTracker()

	updates := updateDistances(t, tracker, 100, true, 99.6, 0.3, 2.0)
	last := updates[len(updates)-1]

	if !last.WrappedStartFinishLine || last.LapCount != 1 || !last.Accepted {
		t.Fatalf("unexpected slow wrap state: %+v", last)
	}
	assertNear(t, last.ForwardDeltaMeters, 2.4)
}

func TestLapContinuityHandlesBackwardMovementWithoutDecrementingLap(t *testing.T) {
	tracker := newTestLapTracker()

	updates := updateDistances(t, tracker, 100, true, 40, 55, 50)
	last := updates[len(updates)-1]

	if !last.Reversed || last.LapIndex != 0 || last.LapCount != 0 || last.WrappedStartFinishLine || last.LargeJump || !last.Accepted {
		t.Fatalf("unexpected reverse state: %+v", last)
	}
	assertNear(t, last.ReverseDeltaMeters, 5)
	assertNear(t, last.TotalForwardMeters, 15)
}

func TestLapContinuityRejectsLargeJumpsAndKeepsPreviousState(t *testing.T) {
	tracker := newTestLapTracker()

	updateDistances(t, tracker, 1000, true, 100, 120)
	state, err := tracker.Update(600, 1000, true)
	if err != nil {
		t.Fatalf("expected update, got %v", err)
	}

	if !state.LargeJump || state.Accepted || state.DistanceMeters != 120 || state.LapCount != 0 {
		t.Fatalf("unexpected large jump state: %+v", state)
	}
	assertNear(t, state.TotalForwardMeters, 20)
}

func TestLapContinuityResetStartsNewSession(t *testing.T) {
	tracker := newTestLapTracker()
	updateDistances(t, tracker, 100, true, 95, 5)

	tracker.Reset()
	state, err := tracker.Update(42, 100, true)
	if err != nil {
		t.Fatalf("expected update after reset, got %v", err)
	}

	if state.LapIndex != 0 || state.LapCount != 0 || state.WrappedStartFinishLine || state.TotalForwardMeters != 0 || !state.Accepted {
		t.Fatalf("unexpected reset state: %+v", state)
	}
	assertNear(t, state.DistanceMeters, 42)
}

func TestLapContinuityOpenCenterlineDoesNotWrap(t *testing.T) {
	tracker := newTestLapTracker()

	updates := updateDistances(t, tracker, 100, false, 95, 100, 0)
	last := updates[len(updates)-1]

	if last.LapCount != 0 || last.WrappedStartFinishLine || !last.LargeJump || last.Accepted {
		t.Fatalf("unexpected open centerline state: %+v", last)
	}
	assertNear(t, last.DistanceMeters, 100)
	assertNear(t, last.Progress, 1)
}

func TestLapContinuityNormalizesClosedDistanceBeforeProgress(t *testing.T) {
	tracker := newTestLapTracker()

	state, err := tracker.Update(105, 100, true)
	if err != nil {
		t.Fatalf("expected update, got %v", err)
	}

	assertNear(t, state.DistanceMeters, 5)
	assertNear(t, state.Progress, 0.05)
}

func TestLapContinuityCanConsumeProjectionAndCenterline(t *testing.T) {
	line := mustCenterline(t, []Point{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}, {X: 0, Y: 0}}, true)
	tracker := newTestLapTracker()

	first, err := UpdateLapContinuity(tracker, line.Project(Point{X: 0, Y: 2}), line)
	if err != nil {
		t.Fatalf("expected projection update, got %v", err)
	}
	second, err := UpdateLapContinuity(tracker, line.Project(Point{X: 2, Y: 0}), line)
	if err != nil {
		t.Fatalf("expected projection update, got %v", err)
	}

	if !first.Accepted || !second.Accepted || !second.WrappedStartFinishLine || second.LapCount != 1 {
		t.Fatalf("unexpected projected continuity states: first=%+v second=%+v", first, second)
	}
}

func newTestLapTracker() *LapContinuityTracker {
	return NewLapContinuityTracker(LapContinuityOptions{
		JitterToleranceMeters: 1,
		MaxForwardDeltaMeters: 40,
		MaxReverseDeltaMeters: 10,
	})
}

func updateDistances(t *testing.T, tracker *LapContinuityTracker, totalMeters float64, closed bool, distances ...float64) []LapContinuityState {
	t.Helper()

	updates := make([]LapContinuityState, 0, len(distances))
	for _, distance := range distances {
		state, err := tracker.Update(distance, totalMeters, closed)
		if err != nil {
			t.Fatalf("expected update for distance %f, got %v", distance, err)
		}
		updates = append(updates, state)
	}

	return updates
}
