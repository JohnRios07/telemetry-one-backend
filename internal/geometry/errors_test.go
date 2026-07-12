package geometry

import (
	"errors"
	"math"
	"testing"
)

func TestNormalizeProgressWithInvalidInputs(t *testing.T) {
	tests := []struct {
		name             string
		distanceFromStart float64
		totalMeters      float64
	}{
		{name: "zero total meters", distanceFromStart: 50, totalMeters: 0},
		{name: "negative total meters", distanceFromStart: 50, totalMeters: -100},
		{name: "negative distance", distanceFromStart: -10, totalMeters: 100},
		{name: "distance past total", distanceFromStart: 150, totalMeters: 100},
		{name: "exact total boundary", distanceFromStart: 100, totalMeters: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			progress := NormalizeProgress(tt.distanceFromStart, tt.totalMeters)
			if progress < 0 || progress > 1 {
				t.Fatalf("expected progress in [0,1] for distance=%f total=%f, got %f",
					tt.distanceFromStart, tt.totalMeters, progress)
			}
		})
	}
}

func TestForwardDeltaInvalidInputs(t *testing.T) {
	tests := []struct {
		name          string
		previousMeters float64
		currentMeters  float64
		totalMeters    float64
		wantErr       error
	}{
		{
			name:          "zero total meters",
			previousMeters: 10,
			currentMeters:  20,
			totalMeters:    0,
			wantErr:        ErrInvalidTrackLength,
		},
		{
			name:          "negative total meters",
			previousMeters: 10,
			currentMeters:  20,
			totalMeters:    -100,
			wantErr:        ErrInvalidTrackLength,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ForwardDelta(tt.previousMeters, tt.currentMeters, tt.totalMeters)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestUpdateLapContinuityWithNilTracker(t *testing.T) {
	line := mustCenterline(t, []Point{{X: 0, Y: 0}, {X: 100, Y: 0}}, false)
	projection := line.Project(Point{X: 50, Y: 0})

	_, err := UpdateLapContinuity(nil, projection, line)
	if err == nil {
		t.Fatal("expected error for nil tracker, got nil")
	}
}

func TestLapContinuityUpdateWithInvalidTrackLength(t *testing.T) {
	tracker := newTestLapTracker()

	_, err := tracker.Update(50, 0, true)
	if !errors.Is(err, ErrInvalidTrackLength) {
		t.Fatalf("expected ErrInvalidTrackLength, got %v", err)
	}
}

func TestLapContinuityUpdateWithNanDistance(t *testing.T) {
	tracker := newTestLapTracker()

	_, err := tracker.Update(math.NaN(), 100, true)
	if !errors.Is(err, ErrInvalidProjectionDistance) {
		t.Fatalf("expected ErrInvalidProjectionDistance, got %v", err)
	}
}
