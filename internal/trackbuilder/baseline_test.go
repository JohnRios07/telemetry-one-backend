package trackbuilder

import (
	"math"
	"testing"

	"telemetry-one-backend/internal/geometry"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

func TestBuildTelemetryBaselineSummarizesAcceptedSamples(t *testing.T) {
	requests := []telemetry.IngestBatchRequest{
		telemetryBaselineRequest("baseline-a", squareLoopFrames(10, 10)),
		telemetryBaselineRequest("baseline-b", squareLoopFrames(12, 10)),
		telemetryBaselineRequest("baseline-rejected", openPathFrames(30)),
	}

	report, err := BuildTelemetryBaseline(requests, "gt7_layout_1240", tracks.OfficialGT7SeedCatalog())
	if err != nil {
		t.Fatalf("BuildTelemetryBaseline returned error: %v", err)
	}
	_, seedLayout, ok := findSeedLayout(tracks.OfficialGT7SeedCatalog(), "gt7_layout_1240")
	if !ok {
		t.Fatal("expected seed layout to resolve")
	}
	if report.Status != telemetryBaselineStatus || report.LayoutID != "gt7_layout_1240" || report.LayoutName != seedLayout.Name {
		t.Fatalf("unexpected report identity: %+v", report)
	}
	if report.SampleCount != 3 || report.AcceptedLapCount != 2 || report.RejectedLapCount != 1 {
		t.Fatalf("unexpected report counts: %+v", report)
	}
	if report.CatalogLengthMeters != seedLayout.LengthMeters {
		t.Fatalf("unexpected catalog length: %.2f want %.2f", report.CatalogLengthMeters, seedLayout.LengthMeters)
	}
	if math.Abs(report.AvgSourcePathLengthMeters-42) > 1e-9 || math.Abs(report.MinSourcePathLengthMeters-40) > 1e-9 || math.Abs(report.MaxSourcePathLengthMeters-44) > 1e-9 || math.Abs(report.StdDevSourcePathLengthMeters-2) > 1e-9 {
		t.Fatalf("unexpected source path stats: %+v", report)
	}
	wantAvgDelta := 42 - seedLayout.LengthMeters
	wantAvgDeltaPct := ((42 / seedLayout.LengthMeters) - 1) * 100
	if math.Abs(report.AvgDeltaMeters-wantAvgDelta) > 1e-9 || math.Abs(report.AvgDeltaPct-wantAvgDeltaPct) > 1e-9 {
		t.Fatalf("unexpected delta stats: %+v", report)
	}
	if report.AcceptanceRule == "" {
		t.Fatal("expected acceptance rule text")
	}
}

func TestBuildTelemetryBaselineRejectsWhenNoAcceptedSamples(t *testing.T) {
	_, err := BuildTelemetryBaseline([]telemetry.IngestBatchRequest{telemetryBaselineRequest("baseline-open", openPathFrames(30))}, "gt7_layout_1240", tracks.OfficialGT7SeedCatalog())
	if err == nil || err != ErrNoAcceptedTelemetryBaselineSamples {
		t.Fatalf("expected no accepted sample error, got %v", err)
	}
}

func TestBuildTelemetryBaselineRejectsMissingLayout(t *testing.T) {
	_, err := BuildTelemetryBaseline([]telemetry.IngestBatchRequest{telemetryBaselineRequest("baseline-a", squareLoopFrames(10, 10))}, "missing-layout", tracks.OfficialGT7SeedCatalog())
	if err == nil || err.Error() == "" {
		t.Fatalf("expected missing-layout error, got %v", err)
	}
}

func telemetryBaselineRequest(sessionID string, frames []telemetry.Frame) telemetry.IngestBatchRequest {
	return telemetry.IngestBatchRequest{SessionID: sessionID, Frames: frames}
}

func squareLoopFrames(width, height float64) []telemetry.Frame {
	points := []geometry.Point{{X: 0, Y: 0, Z: 0}, {X: width, Y: 0, Z: 0}, {X: width, Y: height, Z: 0}, {X: 0, Y: height, Z: 0}, {X: 0, Y: 0, Z: 0}}
	return framesFromPoints(points)
}

func openPathFrames(length float64) []telemetry.Frame {
	points := []geometry.Point{{X: 0, Y: 0, Z: 0}, {X: length / 2, Y: 0, Z: 0}, {X: length, Y: 0, Z: 0}}
	return framesFromPoints(points)
}

func framesFromPoints(points []geometry.Point) []telemetry.Frame {
	frames := make([]telemetry.Frame, 0, len(points))
	for i, point := range points {
		frames = append(frames, telemetry.Frame{TimestampUnixMs: int64(i + 1), PositionX: point.X, PositionY: point.Y, PositionZ: point.Z, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: int64(i), IsOnTrack: true})
	}

	return frames
}
