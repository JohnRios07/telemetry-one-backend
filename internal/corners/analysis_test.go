package corners

import (
	"errors"
	"math"
	"testing"

	"telemetry-one-backend/internal/tracks"
)

func TestAnalyzeCornerCalculatesBaseMetrics(t *testing.T) {
	analysis, err := AnalyzeCorner(analysisCorner(), []AnalysisSample{
		sample(1000, 100, 50, 0.00, 0.00),
		sample(1100, 120, 40, 0.00, 0.60),
		sample(1200, 140, 30, 0.00, 0.20),
		sample(1300, 160, 35, 0.10, 0.00),
		sample(1400, 189.9, 45, 0.60, 0.00),
	}, 1300, true, AnalysisOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if analysis.Status != AnalysisStatusComplete || analysis.SampleCount != 5 {
		t.Fatalf("expected complete analysis for 5 samples, got %+v", analysis)
	}
	assertScalar(t, "entry speed", analysis.EntrySpeedKph, 180)
	assertScalar(t, "minimum speed", analysis.MinimumSpeedKph, 108)
	assertScalar(t, "exit speed", analysis.ExitSpeedKph, 161.99999999999997)
	assertScalar(t, "max brake", analysis.MaxBrakePercent, 60)
	assertScalar(t, "exit acceleration delta", analysis.ExitAccelerationDeltaKph, 54)
	if analysis.FirstThrottleReapplication.Status != MetricStatusAvailable || analysis.FirstThrottleReapplication.DistanceFromStart != 160 || analysis.FirstThrottleReapplication.TimeOffsetMs != 300 {
		t.Fatalf("expected first throttle reapplication at 160m/+300ms, got %+v", analysis.FirstThrottleReapplication)
	}
}

func TestAnalyzeCornerReturnsUnavailableForNoSamples(t *testing.T) {
	analysis, err := AnalyzeCorner(analysisCorner(), nil, 1300, true, AnalysisOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if analysis.Status != AnalysisStatusUnavailable || analysis.Reason != ReasonNoSamples {
		t.Fatalf("expected no_samples unavailable analysis, got %+v", analysis)
	}
}

func TestAnalyzeCornerReturnsUnavailableWhenNoSamplesAreInsideCorner(t *testing.T) {
	analysis, err := AnalyzeCorner(analysisCorner(), []AnalysisSample{sample(1000, 99.9, 50, 0, 0), sample(1100, 190, 50, 0, 0)}, 1300, true, AnalysisOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if analysis.Status != AnalysisStatusUnavailable || analysis.Reason != ReasonNoSamplesInCorner {
		t.Fatalf("expected no_samples_in_corner unavailable analysis, got %+v", analysis)
	}
}

func TestAnalyzeCornerUsesStartInclusiveEndExclusiveBoundaries(t *testing.T) {
	analysis, err := AnalyzeCorner(analysisCorner(), []AnalysisSample{
		sample(1000, 100, 50, 0, 0),
		sample(1100, 190, 20, 1, 1),
	}, 1300, true, AnalysisOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if analysis.SampleCount != 1 {
		t.Fatalf("expected start boundary included and end boundary excluded, got %+v", analysis)
	}
	assertScalar(t, "entry speed", analysis.EntrySpeedKph, 180)
	assertScalar(t, "max brake", analysis.MaxBrakePercent, 0)
}

func TestAnalyzeCornerReportsMissingBrakeAndThrottleData(t *testing.T) {
	analysis, err := AnalyzeCorner(analysisCorner(), []AnalysisSample{
		{TimestampUnixMs: 1000, DistanceFromStart: 100, Progress: 0.1, SpeedMps: 50},
		{TimestampUnixMs: 1100, DistanceFromStart: 150, Progress: 0.12, SpeedMps: 40},
	}, 1300, true, AnalysisOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if analysis.Status != AnalysisStatusPartial {
		t.Fatalf("expected partial analysis when control inputs are missing, got %+v", analysis)
	}
	if analysis.MaxBrakePercent.Reason != ReasonMissingBrakeData {
		t.Fatalf("expected missing brake data, got %+v", analysis.MaxBrakePercent)
	}
	if analysis.FirstThrottleReapplication.Reason != ReasonMissingThrottleData {
		t.Fatalf("expected missing throttle data, got %+v", analysis.FirstThrottleReapplication)
	}
}

func TestAnalyzeCornerReportsNoThrottleReapplication(t *testing.T) {
	analysis, err := AnalyzeCorner(analysisCorner(), []AnalysisSample{
		sample(1000, 100, 50, 0.10, 0),
		sample(1100, 140, 40, 0.10, 0),
		sample(1200, 180, 45, 0.10, 0),
	}, 1300, true, AnalysisOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if analysis.Status != AnalysisStatusPartial || analysis.FirstThrottleReapplication.Reason != ReasonNoThrottleReapply {
		t.Fatalf("expected no throttle reapplication partial metric, got %+v", analysis)
	}
}

func TestAnalyzeCornerSupportsClosedLoopWrapAroundCorner(t *testing.T) {
	corner := tracks.Corner{ID: "synthetic_dev_loop_final", Number: 99, Name: "Synthetic Final Wrap", DefinitionMode: tracks.CornerDefinitionCatalogManual, StartMeters: 1250, ApexMeters: 20, EndMeters: 80}
	analysis, err := AnalyzeCorner(corner, []AnalysisSample{
		sample(1000, 1249.9, 55, 0, 0),
		sample(1100, 1260, 50, 0, 0.30),
		sample(1200, 20, 35, 0, 0.10),
		sample(1300, 60, 42, 0.20, 0),
		sample(1400, 80, 60, 1, 0),
	}, 1300, true, AnalysisOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if analysis.Status != AnalysisStatusComplete || analysis.SampleCount != 3 {
		t.Fatalf("expected wrap-around samples inside final corner only, got %+v", analysis)
	}
	assertScalar(t, "entry speed", analysis.EntrySpeedKph, 180)
	assertScalar(t, "minimum speed", analysis.MinimumSpeedKph, 126)
	assertScalar(t, "exit speed", analysis.ExitSpeedKph, 151.20000000000002)
	if analysis.FirstThrottleReapplication.DistanceFromStart != 60 {
		t.Fatalf("expected throttle reapplication after wrapped apex at 60m, got %+v", analysis.FirstThrottleReapplication)
	}
}

func TestAnalyzeCornerDoesNotUseFutureAutomaticCornerDefinitions(t *testing.T) {
	corner := analysisCorner()
	corner.DefinitionMode = tracks.CornerDefinitionAutoDetectedFuture

	analysis, err := AnalyzeCorner(corner, []AnalysisSample{sample(1000, 100, 50, 0, 0)}, 1300, true, AnalysisOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if analysis.Status != AnalysisStatusUnavailable || analysis.Reason != ReasonUnsupportedCornerDefinition || analysis.SampleCount != 0 {
		t.Fatalf("expected unsupported future automatic corner definition to be unavailable, got %+v", analysis)
	}
}

func TestAnalyzeCornerRejectsInvalidLayoutLength(t *testing.T) {
	if _, err := AnalyzeCorner(analysisCorner(), []AnalysisSample{sample(1000, 100, 50, 0, 0)}, 0, true, AnalysisOptions{}); !errors.Is(err, ErrInvalidAnalysisLength) {
		t.Fatalf("expected invalid layout length error, got %v", err)
	}
}

func analysisCorner() tracks.Corner {
	return tracks.Corner{ID: "synthetic_dev_loop_t1", Number: 1, Name: "Synthetic Turn 1", DefinitionMode: tracks.CornerDefinitionCatalogManual, StartMeters: 100, ApexMeters: 140, EndMeters: 190}
}

func sample(timestamp int64, distance float64, speedMps float64, throttle float64, brake float64) AnalysisSample {
	return AnalysisSample{
		TimestampUnixMs:   timestamp,
		DistanceFromStart: distance,
		Progress:          distance / 1300,
		SpeedMps:          speedMps,
		Throttle:          &throttle,
		Brake:             &brake,
	}
}

func assertScalar(t *testing.T, name string, got ScalarMetric, want float64) {
	t.Helper()
	if got.Status != MetricStatusAvailable || math.Abs(got.Value-want) > 0.000001 {
		t.Fatalf("expected %s %.2f available, got %+v", name, want, got)
	}
}
