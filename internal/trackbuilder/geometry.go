package trackbuilder

import (
	"errors"
	"fmt"
	"math"

	"telemetry-one-backend/internal/geometry"
	"telemetry-one-backend/internal/telemetry"
)

const closedLoopGapThresholdMeters = 25.0

type sampledPoint struct {
	point       geometry.Point
	distance    float64
	accumulated float64
}

func cleanupTelemetryPoints(frames []telemetry.Frame) []geometry.Point {
	points := make([]geometry.Point, 0, len(frames))
	var last geometry.Point
	var hasLast bool

	for _, frame := range frames {
		point := geometry.Point{X: frame.PositionX, Y: frame.PositionY, Z: frame.PositionZ}
		if !isFinite(point.X) || !isFinite(point.Y) || !isFinite(point.Z) {
			continue
		}
		if hasLast && samePoint(point, last) {
			continue
		}
		points = append(points, point)
		last = point
		hasLast = true
	}

	return points
}

func isClosedLoop(points []geometry.Point) bool {
	if len(points) < 3 {
		return false
	}
	gap := geometry.Distance(points[0], points[len(points)-1])
	total := polylineLength(points, false)
	if total <= 0 {
		return false
	}
	return gap <= math.Max(closedLoopGapThresholdMeters, total*0.05)
}

func smoothPoints(points []geometry.Point, window int, closed bool) ([]geometry.Point, error) {
	if window < 1 || window%2 == 0 {
		return nil, ErrInvalidSmoothingWindow
	}
	if len(points) < 2 {
		return nil, ErrInsufficientUsablePoint
	}
	if window == 1 || len(points) == 2 {
		cloned := make([]geometry.Point, len(points))
		copy(cloned, points)
		return cloned, nil
	}

	half := window / 2
	out := make([]geometry.Point, len(points))
	for i := range points {
		var sum geometry.Point
		count := 0
		for offset := -half; offset <= half; offset++ {
			index := i + offset
			if closed {
				index = mod(index, len(points))
			} else if index < 0 {
				index = 0
			} else if index >= len(points) {
				index = len(points) - 1
			}
			sum.X += points[index].X
			sum.Y += points[index].Y
			sum.Z += points[index].Z
			count++
		}
		out[i] = geometry.Point{X: sum.X / float64(count), Y: sum.Y / float64(count), Z: sum.Z / float64(count)}
	}

	return out, nil
}

func rdpSimplify(points []geometry.Point, epsilon float64) []geometry.Point {
	if len(points) <= 2 || epsilon <= 0 {
		cloned := make([]geometry.Point, len(points))
		copy(cloned, points)
		return cloned
	}

	keep := make([]bool, len(points))
	keep[0] = true
	keep[len(points)-1] = true

	var simplify func(start, end int)
	simplify = func(start, end int) {
		if end-start < 2 {
			return
		}
		maxDistance := -1.0
		index := -1
		for i := start + 1; i < end; i++ {
			distance := distanceToSegment(points[i], points[start], points[end])
			if distance > maxDistance {
				maxDistance = distance
				index = i
			}
		}
		if maxDistance > epsilon && index >= 0 {
			keep[index] = true
			simplify(start, index)
			simplify(index, end)
		}
	}

	simplify(0, len(points)-1)

	out := make([]geometry.Point, 0, len(points))
	for i, point := range points {
		if keep[i] {
			out = append(out, point)
		}
	}

	return out
}

func resamplePolyline(points []geometry.Point, step float64, closed bool) ([]sampledPoint, float64, error) {
	if len(points) < 2 {
		return nil, 0, ErrInsufficientUsablePoint
	}
	if !(step > 0) {
		return nil, 0, fmt.Errorf("resample step must be greater than zero")
	}

	segments, total := buildSegments(points, closed)
	if total <= 0 || len(segments) == 0 {
		return nil, 0, errors.New("resampled geometry has zero length")
	}

	targets := make([]float64, 0, int(math.Ceil(total/step))+1)
	if closed {
		for distance := 0.0; distance < total; distance += step {
			targets = append(targets, distance)
		}
	} else {
		for distance := 0.0; distance < total; distance += step {
			targets = append(targets, distance)
		}
		if len(targets) == 0 || targets[len(targets)-1] != total {
			targets = append(targets, total)
		}
	}

	out := make([]sampledPoint, 0, len(targets))
	for _, target := range targets {
		point := interpolateSegments(segments, target)
		out = append(out, sampledPoint{point: point, distance: target, accumulated: target})
	}

	if len(out) < 2 {
		return nil, 0, ErrInsufficientUsablePoint
	}

	return out, total, nil
}

func buildSegments(points []geometry.Point, closed bool) ([]geometry.Segment, float64) {
	if closed && len(points) > 2 && samePoint(points[0], points[len(points)-1]) {
		cloned := make([]geometry.Point, len(points)-1)
		copy(cloned, points[:len(points)-1])
		points = cloned
	}
	centerline, err := geometry.NewCenterline(points, closed)
	if err == nil {
		return centerline.Segments, centerline.TotalMeters
	}

	segments := make([]geometry.Segment, 0, len(points))
	total := 0.0
	for i := 0; i < len(points)-1; i++ {
		length := geometry.Distance(points[i], points[i+1])
		if length == 0 {
			continue
		}
		segments = append(segments, geometry.Segment{Start: points[i], End: points[i+1], StartMeters: total, EndMeters: total + length, Length: length})
		total += length
	}
	if closed {
		length := geometry.Distance(points[len(points)-1], points[0])
		if length > 0 {
			segments = append(segments, geometry.Segment{Start: points[len(points)-1], End: points[0], StartMeters: total, EndMeters: total + length, Length: length})
			total += length
		}
	}

	return segments, total
}

func interpolateSegments(segments []geometry.Segment, distance float64) geometry.Point {
	if len(segments) == 0 {
		return geometry.Point{}
	}
	if distance <= 0 {
		return segments[0].Start
	}
	last := segments[len(segments)-1]
	if distance >= last.EndMeters {
		return last.End
	}
	for _, segment := range segments {
		if distance > segment.EndMeters {
			continue
		}
		ratio := (distance - segment.StartMeters) / segment.Length
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
		return geometry.Point{
			X: segment.Start.X + (segment.End.X-segment.Start.X)*ratio,
			Y: segment.Start.Y + (segment.End.Y-segment.Start.Y)*ratio,
			Z: segment.Start.Z + (segment.End.Z-segment.Start.Z)*ratio,
		}
	}

	return last.End
}

func distanceToSegment(point geometry.Point, start geometry.Point, end geometry.Point) float64 {
	dx := end.X - start.X
	dy := end.Y - start.Y
	dz := end.Z - start.Z
	denominator := dx*dx + dy*dy + dz*dz
	if denominator == 0 {
		return geometry.Distance(point, start)
	}
	ratio := ((point.X-start.X)*dx + (point.Y-start.Y)*dy + (point.Z-start.Z)*dz) / denominator
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	projection := geometry.Point{
		X: start.X + dx*ratio,
		Y: start.Y + dy*ratio,
		Z: start.Z + dz*ratio,
	}

	return geometry.Distance(point, projection)
}

func samePoint(a geometry.Point, b geometry.Point) bool {
	return a.X == b.X && a.Y == b.Y && a.Z == b.Z
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func polylineLength(points []geometry.Point, closed bool) float64 {
	if len(points) < 2 {
		return 0
	}
	total := 0.0
	for i := 0; i < len(points)-1; i++ {
		total += geometry.Distance(points[i], points[i+1])
	}
	if closed {
		total += geometry.Distance(points[len(points)-1], points[0])
	}

	return total
}

func mod(value int, size int) int {
	result := value % size
	if result < 0 {
		result += size
	}

	return result
}
