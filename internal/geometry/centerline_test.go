package geometry

import (
	"errors"
	"math"
	"testing"
)

func TestCenterlineProjectsOntoStraightLine(t *testing.T) {
	line := mustCenterline(t, []Point{{X: 0}, {X: 100}}, false)

	projection := line.Project(Point{X: 25, Y: 3})

	assertNear(t, projection.DistanceFromStart, 25)
	assertNear(t, projection.NormalizedProgress, 0.25)
	assertNear(t, projection.OffCenterlineMeters, 3)
	assertNear(t, projection.Point.X, 25)
	assertNear(t, projection.Point.Y, 0)
}

func TestCenterlineClampsProjectionBeforeAndAfterSegmentEndpoints(t *testing.T) {
	line := mustCenterline(t, []Point{{X: 0}, {X: 100}}, false)

	before := line.Project(Point{X: -10, Y: 4})
	after := line.Project(Point{X: 120, Y: 5})

	assertNear(t, before.DistanceFromStart, 0)
	assertNear(t, before.OffCenterlineMeters, math.Sqrt(116))
	assertNear(t, after.DistanceFromStart, 100)
	assertNear(t, after.NormalizedProgress, 1)
	assertNear(t, after.OffCenterlineMeters, math.Sqrt(425))
}

func TestClosedRectangleCenterlineProjectsAndWrapsProgress(t *testing.T) {
	loop := mustCenterline(t, []Point{{X: 0, Y: 0}, {X: 400, Y: 0}, {X: 400, Y: 250}, {X: 0, Y: 250}, {X: 0, Y: 0}}, true)

	if loop.TotalMeters != 1300 {
		t.Fatalf("expected 1300m synthetic loop, got %f", loop.TotalMeters)
	}
	projection := loop.Project(Point{X: 0, Y: 20})
	delta, err := ForwardDelta(1280, 20, loop.TotalMeters)
	if err != nil {
		t.Fatalf("expected wrapped delta, got %v", err)
	}

	assertNear(t, projection.DistanceFromStart, 1280)
	assertNear(t, projection.NormalizedProgress, 1280.0/1300.0)
	assertNear(t, delta, 40)
}

func TestClosedCenterlineAddsMissingClosingSegment(t *testing.T) {
	loop := mustCenterline(t, []Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}, true)

	projection := loop.Project(Point{X: 0, Y: 5})

	assertNear(t, loop.TotalMeters, 40)
	assertNear(t, projection.DistanceFromStart, 35)
	assertNear(t, projection.NormalizedProgress, 0.875)
}

func TestCenterlineRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name    string
		points  []Point
		wantErr error
	}{
		{name: "too short", points: []Point{{}}, wantErr: ErrCenterlineTooShort},
		{name: "non finite", points: []Point{{}, {X: math.NaN()}}, wantErr: ErrCenterlineInvalidPoint},
		{name: "zero length", points: []Point{{X: 1}, {X: 1}}, wantErr: ErrCenterlineZeroLength},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCenterline(tt.points, true)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func mustCenterline(t *testing.T, points []Point, closed bool) Centerline {
	t.Helper()

	line, err := NewCenterline(points, closed)
	if err != nil {
		t.Fatalf("expected centerline, got %v", err)
	}

	return line
}

func assertNear(t *testing.T, got float64, want float64) {
	t.Helper()

	if math.Abs(got-want) > 0.000001 {
		t.Fatalf("expected %f, got %f", want, got)
	}
}
