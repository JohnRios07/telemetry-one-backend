package trackbuilder

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

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
	_, seedLayout, ok := findSeedLayout(tracks.OfficialGT7SeedCatalog(), "gt7_layout_1240")
	if !ok {
		t.Fatal("expected seed layout to resolve")
	}
	if report.LayoutID != layout.ID || report.LayoutName != layout.Name {
		t.Fatalf("unexpected layout identity in report: %+v", report)
	}
	if report.PointCount != len(layout.CenterLine) || report.TelemetryPathLengthMeters <= 0 || math.IsNaN(report.StartEndGapMeters) {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.CatalogLengthMeters != seedLayout.LengthMeters || report.DeltaMeters != report.TelemetryPathLengthMeters-report.CatalogLengthMeters {
		t.Fatalf("unexpected catalog delta in report: %+v", report)
	}
}

func TestBuildResolvesTsukubaSeedLayout(t *testing.T) {
	request := telemetry.IngestBatchRequest{SessionID: "trackbuilder", Frames: testFramesLoop()}
	catalog, report, err := Build(request, DefaultOptions().NormalizeWithLayout("gt7_layout_471"), tracks.OfficialGT7SeedCatalog())
	if err != nil {
		t.Fatalf("build returned error: %v", err)
	}
	if got := catalog.Tracks[0].ID; got != "gt7_tsukuba_circuit" {
		t.Fatalf("expected Tsukuba track to resolve, got %q", got)
	}
	if got := catalog.Tracks[0].Layouts[0].ID; got != "gt7_layout_471" {
		t.Fatalf("expected Tsukuba layout to resolve, got %q", got)
	}
	if report.PointCount != len(catalog.Tracks[0].Layouts[0].CenterLine) {
		t.Fatalf("unexpected report/catalog mismatch: %+v %+v", report, catalog.Tracks[0].Layouts[0])
	}
}

func TestBuildResolvesBrandsHatchIndySeedLayout(t *testing.T) {
	request := telemetry.IngestBatchRequest{SessionID: "trackbuilder", Frames: testFramesLoop()}
	catalog, report, err := Build(request, DefaultOptions().NormalizeWithLayout("gt7_layout_346"), tracks.OfficialGT7SeedCatalog())
	if err != nil {
		t.Fatalf("build returned error: %v", err)
	}
	if got := catalog.Tracks[0].ID; got != "gt7_brands_hatch" {
		t.Fatalf("expected Brands Hatch track to resolve, got %q", got)
	}
	if got := catalog.Tracks[0].Layouts[0].ID; got != "gt7_layout_346" {
		t.Fatalf("expected Brands Hatch Indy layout to resolve, got %q", got)
	}
	if got := report.LayoutName; got != "Brands Hatch Indy Circuit" {
		t.Fatalf("expected report to preserve layout name, got %q", got)
	}
}

func TestCleanupTelemetryPointsSkipsInvalidPositions(t *testing.T) {
	points := cleanupTelemetryPoints([]telemetry.Frame{
		validTelemetryFrame(1, 0, 0, 0),
		validTelemetryFrame(2, math.NaN(), 1, 0),
		validTelemetryFrame(3, 10, 0, 0),
		validTelemetryFrame(4, math.Inf(1), 0, 0),
	})

	if len(points) != 2 {
		t.Fatalf("expected 2 usable points, got %d: %+v", len(points), points)
	}
	if points[0].X != 0 || points[1].X != 10 {
		t.Fatalf("unexpected cleaned points: %+v", points)
	}
}

func TestBuildFiltersInvalidPointsAndIsDeterministic(t *testing.T) {
	request := telemetry.IngestBatchRequest{
		SessionID: "trackbuilder",
		Frames: []telemetry.Frame{
			validTelemetryFrame(1, 0, 0, 0),
			validTelemetryFrame(2, 10, 0, 0),
			validTelemetryFrame(3, math.NaN(), 1, 0),
			validTelemetryFrame(4, 10, 10, 0),
			validTelemetryFrame(5, 0, 10, 0),
			validTelemetryFrame(6, 0, 0, 0),
		},
	}

	firstCatalog, firstReport, err := Build(request, DefaultOptions().NormalizeWithLayout("gt7_layout_1240"), tracks.OfficialGT7SeedCatalog())
	if err != nil {
		t.Fatalf("first build returned error: %v", err)
	}
	secondCatalog, secondReport, err := Build(request, DefaultOptions().NormalizeWithLayout("gt7_layout_1240"), tracks.OfficialGT7SeedCatalog())
	if err != nil {
		t.Fatalf("second build returned error: %v", err)
	}

	firstEncoded, err := EncodeCatalog(firstCatalog)
	if err != nil {
		t.Fatalf("EncodeCatalog(first) returned error: %v", err)
	}
	secondEncoded, err := EncodeCatalog(secondCatalog)
	if err != nil {
		t.Fatalf("EncodeCatalog(second) returned error: %v", err)
	}
	if !bytes.Equal(firstEncoded, secondEncoded) {
		t.Fatalf("expected identical catalog JSON for identical input\nfirst: %s\nsecond: %s", firstEncoded, secondEncoded)
	}
	if firstReport.TelemetryPathLengthMeters != secondReport.TelemetryPathLengthMeters || firstReport.PointCount != secondReport.PointCount || firstReport.StartEndGapMeters != secondReport.StartEndGapMeters {
		t.Fatalf("expected identical stable report fields, got %+v and %+v", firstReport, secondReport)
	}
	if firstReport.CatalogLengthMeters != secondReport.CatalogLengthMeters || firstReport.DeltaMeters != secondReport.DeltaMeters || firstReport.DeltaPct != secondReport.DeltaPct {
		t.Fatalf("expected identical catalog comparison fields, got %+v and %+v", firstReport, secondReport)
	}
	if math.IsNaN(firstReport.MeanDeviationMeters) != math.IsNaN(secondReport.MeanDeviationMeters) || math.IsNaN(firstReport.MaxDeviationMeters) != math.IsNaN(secondReport.MaxDeviationMeters) {
		t.Fatalf("expected identical deviation NaN state, got %+v and %+v", firstReport, secondReport)
	}

	layout := firstCatalog.Tracks[0].Layouts[0]
	if got := layout.Sources[len(layout.Sources)-1].RetrievedAt; got != time.UnixMilli(request.Frames[0].TimestampUnixMs).UTC().Format("2006-01-02") {
		t.Fatalf("unexpected curated provenance date: got %q", got)
	}
	if len(layout.CenterLine) < 2 {
		t.Fatalf("expected centerline points after filtering invalid input, got %+v", layout.CenterLine)
	}
	if firstReport.PointCount != len(layout.CenterLine) {
		t.Fatalf("report point count should match catalog centerline, got %+v and %d", firstReport, len(layout.CenterLine))
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

func TestBuildAcceptsRequestsLargerThanIngestBatchLimit(t *testing.T) {
	frames := make([]telemetry.Frame, 0, telemetry.MaxBatchFrames+1)
	for i := 0; i < telemetry.MaxBatchFrames+1; i++ {
		frames = append(frames, validTelemetryFrame(int64(i+1), float64(i), 0, 0))
	}

	request := telemetry.IngestBatchRequest{SessionID: "trackbuilder", Frames: frames}
	catalog, report, err := Build(request, DefaultOptions().NormalizeWithLayout("gt7_layout_1240"), tracks.OfficialGT7SeedCatalog())
	if err != nil {
		t.Fatalf("build returned error for long request: %v", err)
	}
	if len(catalog.Tracks) != 1 || len(catalog.Tracks[0].Layouts) != 1 {
		t.Fatalf("expected generated catalog for long request, got %+v", catalog.Tracks)
	}
	if report.PointCount < 2 || report.TelemetryPathLengthMeters <= 0 {
		t.Fatalf("unexpected report for long request: %+v", report)
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
	report := Report{LayoutID: "layout", LayoutName: "Layout", TelemetryPathLengthMeters: 123.45, CatalogLengthMeters: 120.00, DeltaMeters: 3.45, DeltaPct: 2.88, PointCount: 8, StartEndGapMeters: 1.23, MeanDeviationMeters: math.NaN(), MaxDeviationMeters: math.NaN()}
	text := report.String()
	for _, want := range []string{"layoutId:", "layoutName:", "telemetry/generated path length meters:", "catalogLengthMeters:", "deltaMeters:", "deltaPct:", "points:", "start-end gap:", "deviation vs catalog centerline: n/a"} {
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

func validTelemetryFrame(timestamp int64, x, y, z float64) telemetry.Frame {
	return telemetry.Frame{TimestampUnixMs: timestamp, PositionX: x, PositionY: y, PositionZ: z, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: timestamp, IsOnTrack: true}
}

func testFramesSinglePoint() []telemetry.Frame {
	return []telemetry.Frame{{TimestampUnixMs: 1, PositionX: 0, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 0, IsOnTrack: true}}
}

func (o Options) NormalizeWithLayout(layoutID string) Options {
	o.LayoutID = layoutID
	return o.Normalize()
}
