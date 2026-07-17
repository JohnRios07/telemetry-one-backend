package trackbuilder

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"telemetry-one-backend/internal/geometry"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

func TestBuildCuratedCatalogFromIngestBatch(t *testing.T) {
	request := telemetry.IngestBatchRequest{SessionID: "trackbuilder", Frames: testFramesLoop()}
	catalog, report, err := Build(request, DefaultOptions().NormalizeWithLayout("gt7_layout_1240"), tracks.OfficialGT7SeedCatalog())
	if err != nil {
		t.Fatalf("build returned error: %v", err)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("catalog should validate structurally: %v", err)
	}
	if len(catalog.Tracks) != 1 || len(catalog.Tracks[0].Layouts) != 1 {
		t.Fatalf("expected single-track single-layout catalog, got %+v", catalog.Tracks)
	}
	layout := catalog.Tracks[0].Layouts[0]
	if layout.Sources[len(layout.Sources)-1].SourceType != curatedGeometrySourceType {
		t.Fatalf("expected curated provenance, got %+v", layout.Sources)
	}
	if len(layout.Sectors) != 0 || len(layout.Corners) != 0 {
		t.Fatalf("expected empty sectors/corners, got %+v %+v", layout.Sectors, layout.Corners)
	}
	if len(layout.CenterLine) < 2 {
		t.Fatalf("expected centerline points, got %+v", layout.CenterLine)
	}
	if report.PointCount != len(layout.CenterLine) || report.LengthMeters <= 0 || math.IsNaN(report.StartEndGapMeters) {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestBuildRejectsInvalidOptionsAndInsufficientPoints(t *testing.T) {
	request := telemetry.IngestBatchRequest{SessionID: "trackbuilder", Frames: []telemetry.Frame{{TimestampUnixMs: 1, PositionX: 1, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 0, IsOnTrack: true}}}
	_, _, err := Build(request, Options{LayoutID: "gt7_layout_1240", SmoothingWindow: 2, EpsilonMeters: 0.5, ResampleStepMeters: 2}.Normalize(), tracks.OfficialGT7SeedCatalog())
	if err == nil || !strings.Contains(err.Error(), "smoothing-window") {
		t.Fatalf("expected smoothing-window validation error, got %v", err)
	}

	_, _, err = Build(telemetry.IngestBatchRequest{SessionID: "trackbuilder", Frames: testFramesSinglePoint()}, DefaultOptions().NormalizeWithLayout("gt7_layout_1240"), tracks.OfficialGT7SeedCatalog())
	if err == nil || !strings.Contains(err.Error(), "at least two usable points") {
		t.Fatalf("expected insufficient points error, got %v", err)
	}
}

func TestSmoothPointsUsesCircularWrapForClosedLayouts(t *testing.T) {
	points := []geometryPointForTest{{X: 0, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 0}, {X: 10, Y: 10, Z: 0}, {X: 0, Y: 10, Z: 0}, {X: 0, Y: 0, Z: 0}}
	converted := convertTestPoints(points)
	smoothed, err := smoothPoints(converted, 3, true)
	if err != nil {
		t.Fatalf("smoothPoints returned error: %v", err)
	}
	if smoothed[0] == converted[0] || smoothed[len(smoothed)-1] == converted[len(converted)-1] {
		t.Fatalf("expected smoothing to use wrapped neighbors, got %+v", smoothed)
	}
}

func TestRDPAndResampleKeepAccumulatedMetersIncreasing(t *testing.T) {
	points := convertTestPoints([]geometryPointForTest{{X: 0}, {X: 5}, {X: 10}, {X: 15}, {X: 20}})
	simplified := rdpSimplify(points, 0.5)
	resampled, total, err := resamplePolyline(simplified, 2, false)
	if err != nil {
		t.Fatalf("resamplePolyline returned error: %v", err)
	}
	if total <= 0 || len(resampled) < 2 {
		t.Fatalf("unexpected resample result: total=%f points=%+v", total, resampled)
	}
	prev := -1.0
	for _, point := range resampled {
		if point.accumulated <= prev {
			t.Fatalf("expected accumulated meters to increase, got %+v", resampled)
		}
		prev = point.accumulated
	}
}

func TestReportStringIncludesMetricsAndNAToDeviations(t *testing.T) {
	report := Report{LengthMeters: 123.45, PointCount: 8, StartEndGapMeters: 1.23, MeanDeviationMeters: math.NaN(), MaxDeviationMeters: math.NaN()}
	text := report.String()
	for _, want := range []string{"length:", "points:", "start-end gap:", "deviation vs catalog: n/a"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in report %q", want, text)
		}
	}
}

func TestCatalogEncodingProducesJSONOnly(t *testing.T) {
	catalog := tracks.Catalog{CatalogVersion: tracks.CatalogVersionV1, Tracks: []tracks.CatalogTrack{{ID: "track", Name: "Track", Layouts: []tracks.CatalogLayout{{ID: "layout", Name: "Layout", LengthMeters: 10, Sectors: []tracks.Sector{}, Corners: []tracks.Corner{}, CenterLine: []tracks.Point{{Index: 0, X: 0, Y: 0, Z: 0, AccumulatedMeters: 0}, {Index: 1, X: 10, Y: 0, Z: 0, AccumulatedMeters: 10}}}}}}}
	encoded, err := EncodeCatalog(catalog)
	if err != nil {
		t.Fatalf("EncodeCatalog returned error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}
	if _, ok := decoded["report"]; ok {
		t.Fatal("expected catalog JSON only")
	}
}

type geometryPointForTest struct{ X, Y, Z float64 }

func convertTestPoints(points []geometryPointForTest) []geometry.Point {
	converted := make([]geometry.Point, 0, len(points))
	for _, point := range points {
		converted = append(converted, geometry.Point{X: point.X, Y: point.Y, Z: point.Z})
	}
	return converted
}

func testFramesLoop() []telemetry.Frame {
	return []telemetry.Frame{{TimestampUnixMs: 1, PositionX: 0, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 0, IsOnTrack: true}, {TimestampUnixMs: 2, PositionX: 10, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 1, IsOnTrack: true}, {TimestampUnixMs: 3, PositionX: 10, PositionY: 10, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 2, IsOnTrack: true}, {TimestampUnixMs: 4, PositionX: 0, PositionY: 10, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 3, IsOnTrack: true}, {TimestampUnixMs: 5, PositionX: 0, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 4, IsOnTrack: true}}
}

func testFramesSinglePoint() []telemetry.Frame {
	return []telemetry.Frame{{TimestampUnixMs: 1, PositionX: 0, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 0, IsOnTrack: true}}
}

func (o Options) NormalizeWithLayout(layoutID string) Options {
	o.LayoutID = layoutID
	return o.Normalize()
}
