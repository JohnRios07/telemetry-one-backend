package trackbuilder

import (
	"math"
	"sort"

	"telemetry-one-backend/internal/geometry"
)

func sourceRoughnessMetrics(points []geometry.Point) (segmentCount int, minMeters, p50Meters, p95Meters, maxMeters, totalHeadingChangeDegrees, headingChangeDegreesPerMeter float64) {
	if len(points) < 2 {
		return 0, 0, 0, 0, 0, 0, 0
	}

	lengths := make([]float64, 0, len(points)-1)
	totalLength := 0.0
	for i := 0; i < len(points)-1; i++ {
		length := geometry.Distance(points[i], points[i+1])
		if length <= 0 || !isFinite(length) {
			continue
		}
		lengths = append(lengths, length)
		totalLength += length
	}
	if len(lengths) == 0 {
		return 0, 0, 0, 0, 0, 0, 0
	}

	sort.Float64s(lengths)
	segmentCount = len(lengths)
	minMeters = lengths[0]
	p50Meters = percentileSorted(lengths, 0.50)
	p95Meters = percentileSorted(lengths, 0.95)
	maxMeters = lengths[len(lengths)-1]
	totalHeadingChangeDegrees = headingChangeDegrees(points)
	if totalLength > 0 {
		headingChangeDegreesPerMeter = totalHeadingChangeDegrees / totalLength
	}

	return segmentCount, minMeters, p50Meters, p95Meters, maxMeters, totalHeadingChangeDegrees, headingChangeDegreesPerMeter
}

func percentileSorted(values []float64, pct float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return values[0]
	}
	if pct <= 0 {
		return values[0]
	}
	if pct >= 1 {
		return values[len(values)-1]
	}

	position := pct * float64(len(values)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return values[lower]
	}
	weight := position - float64(lower)

	return values[lower] + (values[upper]-values[lower])*weight
}

func headingChangeDegrees(points []geometry.Point) float64 {
	if len(points) < 3 {
		return 0
	}

	totalRadians := 0.0
	previous := vectorBetween(points[0], points[1])
	for i := 1; i < len(points)-1; i++ {
		next := vectorBetween(points[i], points[i+1])
		angle, ok := angleBetween(previous, next)
		if ok {
			totalRadians += angle
		}
		previous = next
	}

	return totalRadians * 180 / math.Pi
}

func vectorBetween(start geometry.Point, end geometry.Point) geometry.Point {
	return geometry.Point{X: end.X - start.X, Y: end.Y - start.Y, Z: end.Z - start.Z}
}

func angleBetween(a geometry.Point, b geometry.Point) (float64, bool) {
	lengthA := math.Sqrt(a.X*a.X + a.Y*a.Y + a.Z*a.Z)
	lengthB := math.Sqrt(b.X*b.X + b.Y*b.Y + b.Z*b.Z)
	if lengthA <= 0 || lengthB <= 0 {
		return 0, false
	}

	cosine := (a.X*b.X + a.Y*b.Y + a.Z*b.Z) / (lengthA * lengthB)
	if cosine < -1 {
		cosine = -1
	}
	if cosine > 1 {
		cosine = 1
	}

	return math.Acos(cosine), true
}
