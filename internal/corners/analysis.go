package corners

import (
	"errors"

	"telemetry-one-backend/internal/tracks"
)

const (
	AnalysisStatusComplete    = "complete"
	AnalysisStatusPartial     = "partial"
	AnalysisStatusUnavailable = "unavailable"

	MetricStatusAvailable   = "available"
	MetricStatusUnavailable = "unavailable"

	ReasonNoSamples              = "no_samples"
	ReasonNoSamplesInCorner      = "no_samples_in_corner"
	ReasonInvalidCornerRange     = "invalid_corner_range"
	ReasonMissingBrakeData       = "missing_brake_data"
	ReasonMissingThrottleData    = "missing_throttle_data"
	ReasonNoThrottleReapply      = "no_throttle_reapplication"
	ReasonInsufficientSamples    = "insufficient_samples"
	ReasonAccelerationNotDerived = "acceleration_not_derived"
)

var ErrInvalidAnalysisLength = errors.New("layout lengthMeters must be greater than zero")

type AnalysisSample struct {
	TimestampUnixMs   int64
	DistanceFromStart float64
	Progress          float64
	SpeedMps          float64
	Throttle          *float64
	Brake             *float64
}

type AnalysisOptions struct {
	BrakeAppliedThreshold    float64
	ThrottleAppliedThreshold float64
}

type ScalarMetric struct {
	Status string  `json:"status"`
	Reason string  `json:"reason,omitempty"`
	Value  float64 `json:"value,omitempty"`
}

type PointMetric struct {
	Status            string  `json:"status"`
	Reason            string  `json:"reason,omitempty"`
	DistanceFromStart float64 `json:"distanceFromStart,omitempty"`
	TimestampUnixMs   int64   `json:"timestampUnixMs,omitempty"`
	TimeOffsetMs      int64   `json:"timeOffsetMs,omitempty"`
}

type Analysis struct {
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
	CornerID    string `json:"cornerId"`
	CornerName  string `json:"cornerName,omitempty"`
	Number      int    `json:"number,omitempty"`
	SampleCount int    `json:"sampleCount"`

	EntrySpeedKph              ScalarMetric `json:"entrySpeedKph"`
	MinimumSpeedKph            ScalarMetric `json:"minimumSpeedKph"`
	ExitSpeedKph               ScalarMetric `json:"exitSpeedKph"`
	MaxBrakePercent            ScalarMetric `json:"maxBrakePercent"`
	FirstThrottleReapplication PointMetric  `json:"firstThrottleReapplication"`
	ExitAccelerationDeltaKph   ScalarMetric `json:"exitAccelerationDeltaKph"`
}

func DefaultAnalysisOptions() AnalysisOptions {
	return AnalysisOptions{
		BrakeAppliedThreshold:    0.05,
		ThrottleAppliedThreshold: 0.05,
	}
}

func AnalyzeCorner(corner tracks.Corner, samples []AnalysisSample, layoutLengthMeters float64, closed bool, options AnalysisOptions) (Analysis, error) {
	if layoutLengthMeters <= 0 {
		return Analysis{}, ErrInvalidAnalysisLength
	}
	if corner.DefinitionMode != tracks.CornerDefinitionCatalogManual {
		return unavailableAnalysis(corner, ReasonUnsupportedCornerDefinition), nil
	}
	if !validCornerRange(corner, layoutLengthMeters) {
		return unavailableAnalysis(corner, ReasonInvalidCornerRange), nil
	}
	if len(samples) == 0 {
		return unavailableAnalysis(corner, ReasonNoSamples), nil
	}

	options = normalizeAnalysisOptions(options)
	inCorner := make([]AnalysisSample, 0, len(samples))
	for _, sample := range samples {
		if !validSample(sample) {
			continue
		}
		distance := sample.DistanceFromStart
		if closed {
			distance = normalizeDistance(distance, layoutLengthMeters)
		}
		if containsDistance(corner, distance, closed) {
			sample.DistanceFromStart = distance
			inCorner = append(inCorner, sample)
		}
	}
	if len(inCorner) == 0 {
		return unavailableAnalysis(corner, ReasonNoSamplesInCorner), nil
	}

	analysis := Analysis{
		Status:                     AnalysisStatusComplete,
		CornerID:                   corner.ID,
		CornerName:                 corner.Name,
		Number:                     corner.Number,
		SampleCount:                len(inCorner),
		EntrySpeedKph:              availableScalar(speedKph(inCorner[0].SpeedMps)),
		ExitSpeedKph:               availableScalar(speedKph(inCorner[len(inCorner)-1].SpeedMps)),
		MinimumSpeedKph:            availableScalar(speedKph(inCorner[0].SpeedMps)),
		MaxBrakePercent:            unavailableScalar(ReasonMissingBrakeData),
		FirstThrottleReapplication: unavailablePoint(ReasonMissingThrottleData),
		ExitAccelerationDeltaKph:   unavailableScalar(ReasonInsufficientSamples),
	}

	minSample := inCorner[0]
	for _, sample := range inCorner[1:] {
		if sample.SpeedMps < minSample.SpeedMps {
			minSample = sample
			analysis.MinimumSpeedKph = availableScalar(speedKph(sample.SpeedMps))
		}
	}

	analysis.MaxBrakePercent = maxBrakeMetric(inCorner)
	analysis.FirstThrottleReapplication = throttleReapplicationMetric(inCorner, corner, options.ThrottleAppliedThreshold)
	analysis.ExitAccelerationDeltaKph = exitAccelerationMetric(inCorner, minSample)

	if hasUnavailableMetric(analysis) {
		analysis.Status = AnalysisStatusPartial
	}

	return analysis, nil
}

func unavailableAnalysis(corner tracks.Corner, reason string) Analysis {
	return Analysis{
		Status:                     AnalysisStatusUnavailable,
		Reason:                     reason,
		CornerID:                   corner.ID,
		CornerName:                 corner.Name,
		Number:                     corner.Number,
		EntrySpeedKph:              unavailableScalar(reason),
		MinimumSpeedKph:            unavailableScalar(reason),
		ExitSpeedKph:               unavailableScalar(reason),
		MaxBrakePercent:            unavailableScalar(reason),
		FirstThrottleReapplication: unavailablePoint(reason),
		ExitAccelerationDeltaKph:   unavailableScalar(reason),
	}
}

func maxBrakeMetric(samples []AnalysisSample) ScalarMetric {
	hasBrake := false
	maxBrake := 0.0
	for _, sample := range samples {
		if sample.Brake == nil || !isFinite(*sample.Brake) {
			continue
		}
		hasBrake = true
		if *sample.Brake > maxBrake {
			maxBrake = *sample.Brake
		}
	}
	if !hasBrake {
		return unavailableScalar(ReasonMissingBrakeData)
	}

	return availableScalar(maxBrake * 100)
}

func throttleReapplicationMetric(samples []AnalysisSample, corner tracks.Corner, threshold float64) PointMetric {
	previousBelow := false
	hasThrottle := false
	entryTimestamp := samples[0].TimestampUnixMs
	for _, sample := range samples {
		if sample.Throttle == nil || !isFinite(*sample.Throttle) {
			continue
		}
		hasThrottle = true
		if !distanceAtOrAfterApex(sample.DistanceFromStart, corner) {
			previousBelow = *sample.Throttle < threshold
			continue
		}
		if previousBelow && *sample.Throttle >= threshold {
			return PointMetric{
				Status:            MetricStatusAvailable,
				DistanceFromStart: sample.DistanceFromStart,
				TimestampUnixMs:   sample.TimestampUnixMs,
				TimeOffsetMs:      sample.TimestampUnixMs - entryTimestamp,
			}
		}
		previousBelow = *sample.Throttle < threshold
	}
	if !hasThrottle {
		return unavailablePoint(ReasonMissingThrottleData)
	}

	return unavailablePoint(ReasonNoThrottleReapply)
}

func exitAccelerationMetric(samples []AnalysisSample, minSample AnalysisSample) ScalarMetric {
	if len(samples) < 2 {
		return unavailableScalar(ReasonInsufficientSamples)
	}
	exit := samples[len(samples)-1]
	if exit.TimestampUnixMs < minSample.TimestampUnixMs {
		return unavailableScalar(ReasonAccelerationNotDerived)
	}

	return availableScalar(speedKph(exit.SpeedMps - minSample.SpeedMps))
}

func hasUnavailableMetric(analysis Analysis) bool {
	return analysis.EntrySpeedKph.Status == MetricStatusUnavailable ||
		analysis.MinimumSpeedKph.Status == MetricStatusUnavailable ||
		analysis.ExitSpeedKph.Status == MetricStatusUnavailable ||
		analysis.MaxBrakePercent.Status == MetricStatusUnavailable ||
		analysis.FirstThrottleReapplication.Status == MetricStatusUnavailable ||
		analysis.ExitAccelerationDeltaKph.Status == MetricStatusUnavailable
}

func validSample(sample AnalysisSample) bool {
	return isFinite(sample.DistanceFromStart) && isFinite(sample.Progress) && isFinite(sample.SpeedMps) && sample.SpeedMps >= 0
}

func validCornerRange(corner tracks.Corner, length float64) bool {
	if !isFinite(corner.StartMeters) || !isFinite(corner.ApexMeters) || !isFinite(corner.EndMeters) {
		return false
	}
	if corner.StartMeters < 0 || corner.ApexMeters < 0 || corner.EndMeters < 0 || corner.StartMeters > length || corner.ApexMeters > length || corner.EndMeters > length {
		return false
	}
	if corner.StartMeters < corner.EndMeters {
		return corner.StartMeters < corner.ApexMeters && corner.ApexMeters < corner.EndMeters
	}
	if corner.StartMeters == corner.EndMeters {
		return false
	}

	return corner.ApexMeters >= corner.StartMeters || corner.ApexMeters < corner.EndMeters
}

func distanceAtOrAfterApex(distance float64, corner tracks.Corner) bool {
	if corner.StartMeters < corner.EndMeters {
		return distance >= corner.ApexMeters
	}
	if corner.ApexMeters >= corner.StartMeters {
		return distance >= corner.ApexMeters || distance < corner.EndMeters
	}

	return distance < corner.EndMeters && distance >= corner.ApexMeters
}

func normalizeAnalysisOptions(options AnalysisOptions) AnalysisOptions {
	defaults := DefaultAnalysisOptions()
	if options.BrakeAppliedThreshold <= 0 || options.BrakeAppliedThreshold > 1 || !isFinite(options.BrakeAppliedThreshold) {
		options.BrakeAppliedThreshold = defaults.BrakeAppliedThreshold
	}
	if options.ThrottleAppliedThreshold <= 0 || options.ThrottleAppliedThreshold > 1 || !isFinite(options.ThrottleAppliedThreshold) {
		options.ThrottleAppliedThreshold = defaults.ThrottleAppliedThreshold
	}

	return options
}

func availableScalar(value float64) ScalarMetric {
	return ScalarMetric{Status: MetricStatusAvailable, Value: value}
}

func unavailableScalar(reason string) ScalarMetric {
	return ScalarMetric{Status: MetricStatusUnavailable, Reason: reason}
}

func unavailablePoint(reason string) PointMetric {
	return PointMetric{Status: MetricStatusUnavailable, Reason: reason}
}

func speedKph(speedMps float64) float64 {
	return speedMps * 3.6
}
