package trackbuilder

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"telemetry-one-backend/internal/geometry"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

const telemetryBaselineStatus = "telemetry_baseline_only"

var ErrNoAcceptedTelemetryBaselineSamples = errors.New("telemetry baseline requires at least one accepted clean lap")
var ErrNoTelemetryBaselineSamples = errors.New("telemetry baseline requires at least one input batch")

type TelemetryBaselineReport struct {
	Status                       string  `json:"status"`
	LayoutID                     string  `json:"layoutId"`
	LayoutName                   string  `json:"layoutName"`
	CatalogLengthMeters          float64 `json:"catalogLengthMeters"`
	SampleCount                  int     `json:"sampleCount"`
	AcceptedLapCount             int     `json:"acceptedLapCount"`
	RejectedLapCount             int     `json:"rejectedLapCount"`
	AvgSourcePathLengthMeters    float64 `json:"avgSourcePathLengthMeters"`
	MinSourcePathLengthMeters    float64 `json:"minSourcePathLengthMeters"`
	MaxSourcePathLengthMeters    float64 `json:"maxSourcePathLengthMeters"`
	StdDevSourcePathLengthMeters float64 `json:"stddevSourcePathLengthMeters"`
	AvgDeltaMeters               float64 `json:"avgDeltaMeters"`
	AvgDeltaPct                  float64 `json:"avgDeltaPct"`
	AcceptanceRule               string  `json:"acceptanceRule"`
}

func (r TelemetryBaselineReport) String() string {
	var b strings.Builder
	fmt.Fprintln(&b, "trackbuilder observed telemetry baseline")
	fmt.Fprintf(&b, "status: %s\n", r.Status)
	fmt.Fprintf(&b, "layoutId: %s\n", r.LayoutID)
	fmt.Fprintf(&b, "layoutName: %s\n", r.LayoutName)
	fmt.Fprintf(&b, "catalogLengthMeters: %.2fm\n", r.CatalogLengthMeters)
	fmt.Fprintf(&b, "sampleCount: %d\n", r.SampleCount)
	fmt.Fprintf(&b, "acceptedLapCount: %d\n", r.AcceptedLapCount)
	fmt.Fprintf(&b, "rejectedLapCount: %d\n", r.RejectedLapCount)
	fmt.Fprintf(&b, "avgSourcePathLengthMeters: %.2fm\n", r.AvgSourcePathLengthMeters)
	fmt.Fprintf(&b, "minSourcePathLengthMeters: %.2fm\n", r.MinSourcePathLengthMeters)
	fmt.Fprintf(&b, "maxSourcePathLengthMeters: %.2fm\n", r.MaxSourcePathLengthMeters)
	fmt.Fprintf(&b, "stddevSourcePathLengthMeters: %.2fm\n", r.StdDevSourcePathLengthMeters)
	fmt.Fprintf(&b, "avgDeltaMeters: %.2fm\n", r.AvgDeltaMeters)
	fmt.Fprintf(&b, "avgDeltaPct: %.2f%%\n", r.AvgDeltaPct)
	fmt.Fprintf(&b, "acceptanceRule: %s\n", r.AcceptanceRule)

	return b.String()
}

func BuildTelemetryBaseline(requests []telemetry.IngestBatchRequest, layoutID string, seed tracks.Catalog) (TelemetryBaselineReport, error) {
	if strings.TrimSpace(layoutID) == "" {
		return TelemetryBaselineReport{}, ErrMissingLayoutID
	}
	if len(requests) == 0 {
		return TelemetryBaselineReport{}, ErrNoTelemetryBaselineSamples
	}

	_, layout, ok := findSeedLayout(seed, layoutID)
	if !ok {
		return TelemetryBaselineReport{}, fmt.Errorf("%w: %s", errLayoutNotFound, layoutID)
	}

	sourceLengths := make([]float64, 0, len(requests))
	deltas := make([]float64, 0, len(requests))
	deltaPcts := make([]float64, 0, len(requests))
	for _, request := range requests {
		if err := validateBuildRequest(request); err != nil {
			return TelemetryBaselineReport{}, fmt.Errorf("validate ingest batch: %w", err)
		}
		cleaned := cleanupTelemetryPoints(request.Frames)
		if !isTelemetryBaselineSampleAccepted(cleaned) {
			continue
		}

		sourcePathLengthMeters := polylineLength(cleaned, false)
		sourceLengths = append(sourceLengths, sourcePathLengthMeters)
		deltas = append(deltas, sourcePathLengthMeters-layout.LengthMeters)
		deltaPcts = append(deltaPcts, deltaPct(sourcePathLengthMeters, layout.LengthMeters))
	}

	if len(sourceLengths) == 0 {
		return TelemetryBaselineReport{}, ErrNoAcceptedTelemetryBaselineSamples
	}

	avgSource, minSource, maxSource, stddevSource := summarizeFloatSeries(sourceLengths)
	avgDelta, _, _, _ := summarizeFloatSeries(deltas)
	avgDeltaPct, _, _, _ := summarizeFloatSeries(deltaPcts)

	return TelemetryBaselineReport{
		Status:                       telemetryBaselineStatus,
		LayoutID:                     layout.ID,
		LayoutName:                   layout.Name,
		CatalogLengthMeters:          layout.LengthMeters,
		SampleCount:                  len(requests),
		AcceptedLapCount:             len(sourceLengths),
		RejectedLapCount:             len(requests) - len(sourceLengths),
		AvgSourcePathLengthMeters:    avgSource,
		MinSourcePathLengthMeters:    minSource,
		MaxSourcePathLengthMeters:    maxSource,
		StdDevSourcePathLengthMeters: stddevSource,
		AvgDeltaMeters:               avgDelta,
		AvgDeltaPct:                  avgDeltaPct,
		AcceptanceRule:               "accepted when cleaned telemetry has at least 3 usable points, positive path length, and a start-end gap within the existing closed-loop threshold (<= max(25m, 5% of path length))",
	}, nil
}

func isTelemetryBaselineSampleAccepted(cleaned []geometry.Point) bool {
	if len(cleaned) < 3 {
		return false
	}
	if !isClosedLoop(cleaned) {
		return false
	}
	pathLength := polylineLength(cleaned, false)
	return pathLength > 0 && isFinite(pathLength)
}

func summarizeFloatSeries(values []float64) (avg, min, max, stddev float64) {
	if len(values) == 0 {
		return math.NaN(), math.NaN(), math.NaN(), math.NaN()
	}
	min = values[0]
	max = values[0]
	mean := 0.0
	m2 := 0.0
	for i, value := range values {
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
		delta := value - mean
		mean += delta / float64(i+1)
		m2 += delta * (value - mean)
	}
	avg = mean
	stddev = 0
	if len(values) > 0 {
		stddev = math.Sqrt(m2 / float64(len(values)))
	}

	return avg, min, max, stddev
}
