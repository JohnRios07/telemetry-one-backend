package trackbuilder

import (
	"math"
	"sort"

	"telemetry-one-backend/internal/geometry"
)

const planarLengthEpsilon = 1e-9
const sourceChordExcessWindowPoints = 4

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

func sourceChordExcessMetrics(points []geometry.Point) (windowCount int, ratioAvg, ratioP50, ratioP95, ratioMax, excessAvg, excessP50, excessP95, excessMax float64) {
	if len(points) < sourceChordExcessWindowPoints {
		return 0, math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN()
	}

	analysis := points
	if len(analysis) > 2 && samePoint(analysis[0], analysis[len(analysis)-1]) {
		analysis = analysis[:len(analysis)-1]
	}
	if len(analysis) < sourceChordExcessWindowPoints {
		return 0, math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN()
	}

	ratios := make([]float64, 0, len(analysis)-sourceChordExcessWindowPoints+1)
	excesses := make([]float64, 0, len(analysis)-sourceChordExcessWindowPoints+1)
	for i := 0; i+sourceChordExcessWindowPoints <= len(analysis); i++ {
		pathMeters, chordMeters, ok := planarWindowPathAndChord(analysis[i : i+sourceChordExcessWindowPoints])
		if !ok {
			continue
		}
		ratio := pathMeters / chordMeters
		if !isFinite(ratio) {
			continue
		}
		ratios = append(ratios, ratio)
		excesses = append(excesses, pathMeters-chordMeters)
	}

	if len(ratios) == 0 {
		return 0, math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN()
	}

	windowCount = len(ratios)
	ratioAvg, ratioP50, ratioP95, ratioMax = summarizeWindowSeries(ratios)
	excessAvg, excessP50, excessP95, excessMax = summarizeWindowSeries(excesses)

	return windowCount, ratioAvg, ratioP50, ratioP95, ratioMax, excessAvg, excessP50, excessP95, excessMax
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

func planarWindowPathAndChord(points []geometry.Point) (pathMeters, chordMeters float64, ok bool) {
	if len(points) < 2 {
		return 0, 0, false
	}

	for i := 0; i < len(points)-1; i++ {
		segment := planarDistanceBetween(points[i], points[i+1])
		if !isFinite(segment) || segment <= planarLengthEpsilon {
			return 0, 0, false
		}
		pathMeters += segment
	}
	chordMeters = planarDistanceBetween(points[0], points[len(points)-1])
	if !isFinite(pathMeters) || !isFinite(chordMeters) || chordMeters <= planarLengthEpsilon {
		return 0, 0, false
	}

	return pathMeters, chordMeters, true
}

func planarDistanceBetween(a geometry.Point, b geometry.Point) float64 {
	return math.Hypot(b.X-a.X, b.Z-a.Z)
}

func summarizeWindowSeries(values []float64) (avg, p50, p95, max float64) {
	if len(values) == 0 {
		return math.NaN(), math.NaN(), math.NaN(), math.NaN()
	}

	avg, _, max, _ = summarizeFloatSeries(values)
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	p50 = percentileSorted(sorted, 0.50)
	p95 = percentileSorted(sorted, 0.95)

	return avg, p50, p95, max
}
