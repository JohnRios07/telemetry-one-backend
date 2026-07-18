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
	if report.SourcePointCount != len(testFramesLoop()) || report.SimplifiedPointCount < 2 || report.GeneratedPointCount != len(layout.CenterLine) || report.GeneratedPathLengthMeters <= 0 || report.SourcePathLengthMeters <= 0 || math.IsNaN(report.StartEndGapMeters) {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.CatalogLengthMeters != seedLayout.LengthMeters || report.DeltaMeters != report.GeneratedPathLengthMeters-report.CatalogLengthMeters {
		t.Fatalf("unexpected catalog delta in report: %+v", report)
	}
	if report.SourceLocalChordWindowCount == 0 || report.SourceLocalChordWindowPoints != 4 {
		t.Fatalf("expected local chord diagnostics in report: %+v", report)
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
	if report.SimplifiedPointCount > report.SourcePointCount || report.GeneratedPointCount != len(catalog.Tracks[0].Layouts[0].CenterLine) {
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

func TestSourceRoughnessMetricsSummarizeShortAndJaggedPaths(t *testing.T) {
	t.Run("short inputs", func(t *testing.T) {
		if count, min, p50, p95, max, heading, perMeter := sourceRoughnessMetrics(nil); count != 0 || min != 0 || p50 != 0 || p95 != 0 || max != 0 || heading != 0 || perMeter != 0 {
			t.Fatalf("expected zero metrics for empty input, got %d %.2f %.2f %.2f %.2f %.2f %.4f", count, min, p50, p95, max, heading, perMeter)
		}
		if count, min, p50, p95, max, heading, perMeter := sourceRoughnessMetrics([]geometry.Point{{X: 1, Y: 2, Z: 3}}); count != 0 || min != 0 || p50 != 0 || p95 != 0 || max != 0 || heading != 0 || perMeter != 0 {
			t.Fatalf("expected zero metrics for single point input, got %d %.2f %.2f %.2f %.2f %.2f %.4f", count, min, p50, p95, max, heading, perMeter)
		}
	})

	t.Run("jagged path", func(t *testing.T) {
		points := []geometry.Point{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 1}, {X: 2, Y: 0, Z: 1}, {X: 2, Y: 0, Z: 2}}
		count, min, p50, p95, max, heading, perMeter := sourceRoughnessMetrics(points)
		if count != 4 {
			t.Fatalf("expected 4 segments, got %d", count)
		}
		for _, got := range []float64{min, p50, p95, max} {
			if got != 1 {
				t.Fatalf("expected unit segment lengths, got min=%.2f p50=%.2f p95=%.2f max=%.2f", min, p50, p95, max)
			}
		}
		if heading != 270 {
			t.Fatalf("expected 270 degrees of heading change, got %.2f", heading)
		}
		if perMeter != 67.5 {
			t.Fatalf("expected 67.5 degrees per meter, got %.4f", perMeter)
		}
	})

	t.Run("vertical changes do not add planar heading change", func(t *testing.T) {
		points := []geometry.Point{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}, {X: 1, Y: 10, Z: 0}, {X: 2, Y: 10, Z: 0}}
		count, _, _, _, _, heading, perMeter := sourceRoughnessMetrics(points)
		if count != 3 {
			t.Fatalf("expected 3 segments, got %d", count)
		}
		if heading != 0 || perMeter != 0 {
			t.Fatalf("expected vertical-only changes to produce zero planar heading change, got heading=%.4f perMeter=%.4f", heading, perMeter)
		}
	})
}

func TestSourceChordExcessMetricsSummarizeLocalWindowedPaths(t *testing.T) {
	t.Run("short inputs", func(t *testing.T) {
		if count, ratioAvg, ratioP50, ratioP95, ratioMax, excessAvg, excessP50, excessP95, excessMax := sourceChordExcessMetrics(nil); count != 0 || !math.IsNaN(ratioAvg) || !math.IsNaN(ratioP50) || !math.IsNaN(ratioP95) || !math.IsNaN(ratioMax) || !math.IsNaN(excessAvg) || !math.IsNaN(excessP50) || !math.IsNaN(excessP95) || !math.IsNaN(excessMax) {
			t.Fatalf("expected NaN metrics for empty input, got %d %.4f %.4f %.4f %.4f %.4f %.4f %.4f %.4f", count, ratioAvg, ratioP50, ratioP95, ratioMax, excessAvg, excessP50, excessP95, excessMax)
		}
		if count, ratioAvg, ratioP50, ratioP95, ratioMax, excessAvg, excessP50, excessP95, excessMax := sourceChordExcessMetrics([]geometry.Point{{X: 1, Y: 2, Z: 3}, {X: 2, Y: 2, Z: 3}, {X: 3, Y: 2, Z: 3}}); count != 0 || !math.IsNaN(ratioAvg) || !math.IsNaN(ratioP50) || !math.IsNaN(ratioP95) || !math.IsNaN(ratioMax) || !math.IsNaN(excessAvg) || !math.IsNaN(excessP50) || !math.IsNaN(excessP95) || !math.IsNaN(excessMax) {
			t.Fatalf("expected NaN metrics for insufficient input, got %d %.4f %.4f %.4f %.4f %.4f %.4f %.4f %.4f", count, ratioAvg, ratioP50, ratioP95, ratioMax, excessAvg, excessP50, excessP95, excessMax)
		}
	})

	t.Run("square path", func(t *testing.T) {
		points := []geometry.Point{{X: 0, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 0}, {X: 10, Y: 0, Z: 10}, {X: 0, Y: 0, Z: 10}, {X: 0, Y: 0, Z: 0}}
		count, ratioAvg, ratioP50, ratioP95, ratioMax, excessAvg, excessP50, excessP95, excessMax := sourceChordExcessMetrics(points)
		if count != 1 {
			t.Fatalf("expected 1 local window, got %d", count)
		}
		if ratioAvg != 3 || ratioP50 != 3 || ratioP95 != 3 || ratioMax != 3 {
			t.Fatalf("expected ratio 3 for square window, got avg=%.4f p50=%.4f p95=%.4f max=%.4f", ratioAvg, ratioP50, ratioP95, ratioMax)
		}
		if excessAvg != 20 || excessP50 != 20 || excessP95 != 20 || excessMax != 20 {
			t.Fatalf("expected excess 20 for square window, got avg=%.4f p50=%.4f p95=%.4f max=%.4f", excessAvg, excessP50, excessP95, excessMax)
		}
	})
}

func TestBuildFiltersInvalidPointsAndIsDeterministic(t *testing.T) {
	request := telemetry.IngestBatchRequest{
		SessionID: "trackbuilder",
		Frames: []telemetry.Frame{
			validTelemetryFrame(1, 0, 0, 0),
			validTelemetryFrame(2, 10, 0, 0),
			validTelemetryFrame(3, math.NaN(), 0, 1),
			validTelemetryFrame(4, 10, 0, 10),
			validTelemetryFrame(5, 0, 0, 10),
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
	if firstReport.SourcePathLengthMeters != secondReport.SourcePathLengthMeters || firstReport.GeneratedPathLengthMeters != secondReport.GeneratedPathLengthMeters || firstReport.SourcePointCount != secondReport.SourcePointCount || firstReport.SimplifiedPointCount != secondReport.SimplifiedPointCount || firstReport.GeneratedPointCount != secondReport.GeneratedPointCount || firstReport.StartEndGapMeters != secondReport.StartEndGapMeters {
		t.Fatalf("expected identical stable report fields, got %+v and %+v", firstReport, secondReport)
	}
	if firstReport.CatalogLengthMeters != secondReport.CatalogLengthMeters || firstReport.DeltaMeters != secondReport.DeltaMeters || firstReport.DeltaPct != secondReport.DeltaPct {
		t.Fatalf("expected identical catalog comparison fields, got %+v and %+v", firstReport, secondReport)
	}
	if firstReport.SourceSegmentCount != secondReport.SourceSegmentCount || firstReport.SourceSegmentLengthMinMeters != secondReport.SourceSegmentLengthMinMeters || firstReport.SourceSegmentLengthP50Meters != secondReport.SourceSegmentLengthP50Meters || firstReport.SourceSegmentLengthP95Meters != secondReport.SourceSegmentLengthP95Meters || firstReport.SourceSegmentLengthMaxMeters != secondReport.SourceSegmentLengthMaxMeters || firstReport.SourceHeadingChangeDegrees != secondReport.SourceHeadingChangeDegrees || firstReport.SourceHeadingChangePerMeter != secondReport.SourceHeadingChangePerMeter {
		t.Fatalf("expected identical roughness metrics, got %+v and %+v", firstReport, secondReport)
	}
	if firstReport.SourceLocalChordWindowPoints != secondReport.SourceLocalChordWindowPoints || firstReport.SourceLocalChordWindowCount != secondReport.SourceLocalChordWindowCount || firstReport.SourceLocalChordRatioAvg != secondReport.SourceLocalChordRatioAvg || firstReport.SourceLocalChordRatioP50 != secondReport.SourceLocalChordRatioP50 || firstReport.SourceLocalChordRatioP95 != secondReport.SourceLocalChordRatioP95 || firstReport.SourceLocalChordRatioMax != secondReport.SourceLocalChordRatioMax || firstReport.SourceLocalChordExcessAvg != secondReport.SourceLocalChordExcessAvg || firstReport.SourceLocalChordExcessP50 != secondReport.SourceLocalChordExcessP50 || firstReport.SourceLocalChordExcessP95 != secondReport.SourceLocalChordExcessP95 || firstReport.SourceLocalChordExcessMax != secondReport.SourceLocalChordExcessMax {
		t.Fatalf("expected identical local chord metrics, got %+v and %+v", firstReport, secondReport)
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
	if firstReport.SimplificationDroppedPoints != firstReport.SourcePointCount-firstReport.SimplifiedPointCount || firstReport.GeneratedPointCount != len(layout.CenterLine) {
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
	if report.SimplifiedPointCount < 2 || report.GeneratedPointCount < 2 || report.GeneratedPathLengthMeters <= 0 || report.SourcePathLengthMeters <= 0 {
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
	report := Report{LayoutID: "layout", LayoutName: "Layout", SourcePointCount: 5, SourceSegmentCount: 4, SourceSegmentLengthMinMeters: 0.10, SourceSegmentLengthP50Meters: 1.20, SourceSegmentLengthP95Meters: 2.30, SourceSegmentLengthMaxMeters: 3.40, SourceHeadingChangeDegrees: 45.67, SourceHeadingChangePerMeter: 0.12, SourceLocalChordWindowPoints: 4, SourceLocalChordWindowCount: 1, SourceLocalChordRatioAvg: 1.23, SourceLocalChordRatioP50: 1.20, SourceLocalChordRatioP95: 1.30, SourceLocalChordRatioMax: 1.40, SourceLocalChordExcessAvg: 0.56, SourceLocalChordExcessP50: 0.50, SourceLocalChordExcessP95: 0.70, SourceLocalChordExcessMax: 0.80, SourcePathLengthMeters: 120.12, SimplifiedPointCount: 3, SimplificationDroppedPoints: 2, GeneratedPointCount: 8, GeneratedPathLengthMeters: 123.45, CatalogLengthMeters: 120.00, DeltaMeters: 3.45, DeltaPct: 2.88, StartEndGapMeters: 1.23, MeanDeviationMeters: math.NaN(), MaxDeviationMeters: math.NaN()}
	text := report.String()
	for _, want := range []string{"layoutId:", "layoutName:", "source point count:", "source segment count:", "source segment length meters:", "source planar heading change (X/Z):", "source local chord excess (4-point windows):", "source path length meters:", "simplified point count:", "simplification dropped points:", "generated point count:", "generated path length meters:", "catalogLengthMeters:", "deltaMeters:", "deltaPct:", "start-end gap:", "deviation vs catalog centerline: n/a"} {
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
	return []telemetry.Frame{{TimestampUnixMs: 1, PositionX: 0, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 0, IsOnTrack: true}, {TimestampUnixMs: 2, PositionX: 10, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 1, IsOnTrack: true}, {TimestampUnixMs: 3, PositionX: 10, PositionY: 0, PositionZ: 10, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 2, IsOnTrack: true}, {TimestampUnixMs: 4, PositionX: 0, PositionY: 0, PositionZ: 10, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 3, IsOnTrack: true}, {TimestampUnixMs: 5, PositionX: 0, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 4, IsOnTrack: true}}
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
