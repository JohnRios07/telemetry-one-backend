package trackbuilder

import (
	"math"
	"sort"

	"telemetry-one-backend/internal/geometry"
)

const planarLengthEpsilon = 1e-9

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
	previous, ok := planarVectorBetween(points[0], points[1])
	if !ok {
		for i := 1; i < len(points)-1; i++ {
			candidate, candidateOK := planarVectorBetween(points[i], points[i+1])
			if candidateOK {
				previous = candidate
				ok = true
				break
			}
		}
	}
	if !ok {
		return 0
	}

	for i := 1; i < len(points)-1; i++ {
		next, nextOK := planarVectorBetween(points[i], points[i+1])
		if !nextOK {
			continue
		}
		angle, ok := angleBetween(previous, next)
		if ok {
			totalRadians += angle
		}
		previous = next
	}

	return totalRadians * 180 / math.Pi
}

func planarVectorBetween(start geometry.Point, end geometry.Point) (geometry.Point, bool) {
	v := geometry.Point{X: end.X - start.X, Z: end.Z - start.Z}
	if math.Hypot(v.X, v.Z) <= planarLengthEpsilon {
		return geometry.Point{}, false
	}

	return v, true
}

func angleBetween(a geometry.Point, b geometry.Point) (float64, bool) {
	lengthA := math.Hypot(a.X, a.Z)
	lengthB := math.Hypot(b.X, b.Z)
	if lengthA <= planarLengthEpsilon || lengthB <= planarLengthEpsilon {
		return 0, false
	}

	dot := a.X*b.X + a.Z*b.Z
	cross := a.X*b.Z - a.Z*b.X

	return math.Atan2(math.Abs(cross), dot), true
}
