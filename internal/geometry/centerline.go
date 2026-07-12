package geometry

import (
	"errors"
	"fmt"
	"math"
)

var (
	ErrCenterlineTooShort     = errors.New("centerline must contain at least two points")
	ErrCenterlineInvalidPoint = errors.New("centerline point coordinates must be finite")
	ErrCenterlineZeroLength   = errors.New("centerline total length must be greater than zero")
	ErrInvalidTrackLength     = errors.New("track length must be greater than zero")
)

type Point struct {
	X float64
	Y float64
	Z float64
}

type Centerline struct {
	Points      []Point
	Segments    []Segment
	TotalMeters float64
	Closed      bool
}

type Segment struct {
	Start       Point
	End         Point
	StartMeters float64
	EndMeters   float64
	Length      float64
}

type Projection struct {
	SegmentIndex        int
	SegmentRatio        float64
	Point               Point
	DistanceFromStart   float64
	NormalizedProgress  float64
	OffCenterlineMeters float64
}

func NewCenterline(points []Point, closed bool) (Centerline, error) {
	if len(points) < 2 {
		return Centerline{}, ErrCenterlineTooShort
	}

	cloned := make([]Point, len(points))
	copy(cloned, points)
	for i, point := range cloned {
		if !isFinite(point.X) || !isFinite(point.Y) || !isFinite(point.Z) {
			return Centerline{}, fmt.Errorf("points[%d]: %w", i, ErrCenterlineInvalidPoint)
		}
	}

	segments := make([]Segment, 0, len(cloned))
	total := 0.0
	for i := 0; i < len(cloned)-1; i++ {
		segment, ok := newSegment(cloned[i], cloned[i+1], total)
		if !ok {
			continue
		}
		segments = append(segments, segment)
		total = segment.EndMeters
	}
	if closed && !samePoint(cloned[0], cloned[len(cloned)-1]) {
		segment, ok := newSegment(cloned[len(cloned)-1], cloned[0], total)
		if ok {
			segments = append(segments, segment)
			total = segment.EndMeters
		}
	}
	if total <= 0 || len(segments) == 0 {
		return Centerline{}, ErrCenterlineZeroLength
	}

	return Centerline{Points: cloned, Segments: segments, TotalMeters: total, Closed: closed}, nil
}

func (c Centerline) Project(point Point) Projection {
	best := Projection{SegmentIndex: -1, OffCenterlineMeters: math.Inf(1)}
	for i, segment := range c.Segments {
		ratio, projected := projectOnSegment(point, segment)
		distance := Distance(point, projected)
		if distance >= best.OffCenterlineMeters {
			continue
		}

		distanceFromStart := segment.StartMeters + segment.Length*ratio
		if c.Closed {
			distanceFromStart = normalizeDistance(distanceFromStart, c.TotalMeters)
		}
		best = Projection{
			SegmentIndex:        i,
			SegmentRatio:        ratio,
			Point:               projected,
			DistanceFromStart:   distanceFromStart,
			NormalizedProgress:  normalizedProgress(distanceFromStart, c.TotalMeters, c.Closed),
			OffCenterlineMeters: distance,
		}
	}

	return best
}

func NormalizeProgress(distanceFromStart float64, totalMeters float64) float64 {
	return normalizedProgress(distanceFromStart, totalMeters, true)
}

func normalizedProgress(distanceFromStart float64, totalMeters float64, closed bool) float64 {
	if totalMeters <= 0 {
		return 0
	}
	if !closed {
		if distanceFromStart <= 0 {
			return 0
		}
		if distanceFromStart >= totalMeters {
			return 1
		}

		return distanceFromStart / totalMeters
	}

	return normalizeDistance(distanceFromStart, totalMeters) / totalMeters
}

func ForwardDelta(previousMeters float64, currentMeters float64, totalMeters float64) (float64, error) {
	if totalMeters <= 0 {
		return 0, ErrInvalidTrackLength
	}

	previous := normalizeDistance(previousMeters, totalMeters)
	current := normalizeDistance(currentMeters, totalMeters)
	if current >= previous {
		return current - previous, nil
	}

	return totalMeters - previous + current, nil
}

func Distance(a Point, b Point) float64 {
	dx := b.X - a.X
	dy := b.Y - a.Y
	dz := b.Z - a.Z

	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func newSegment(start Point, end Point, startMeters float64) (Segment, bool) {
	length := Distance(start, end)
	if length == 0 {
		return Segment{}, false
	}

	return Segment{Start: start, End: end, StartMeters: startMeters, EndMeters: startMeters + length, Length: length}, true
}

func projectOnSegment(point Point, segment Segment) (float64, Point) {
	dx := segment.End.X - segment.Start.X
	dy := segment.End.Y - segment.Start.Y
	dz := segment.End.Z - segment.Start.Z
	denominator := dx*dx + dy*dy + dz*dz
	if denominator == 0 {
		return 0, segment.Start
	}

	ratio := ((point.X-segment.Start.X)*dx + (point.Y-segment.Start.Y)*dy + (point.Z-segment.Start.Z)*dz) / denominator
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	return ratio, Point{
		X: segment.Start.X + dx*ratio,
		Y: segment.Start.Y + dy*ratio,
		Z: segment.Start.Z + dz*ratio,
	}
}

func normalizeDistance(distance float64, totalMeters float64) float64 {
	if totalMeters <= 0 {
		return 0
	}

	normalized := math.Mod(distance, totalMeters)
	if normalized < 0 {
		normalized += totalMeters
	}
	if normalized == totalMeters {
		return 0
	}

	return normalized
}

func samePoint(a Point, b Point) bool {
	return a.X == b.X && a.Y == b.Y && a.Z == b.Z
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
