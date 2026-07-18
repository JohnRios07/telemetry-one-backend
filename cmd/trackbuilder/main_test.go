package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/trackbuilder"
	"telemetry-one-backend/internal/tracks"
)

func TestRunWritesCatalogJSONToStdoutAndReportToStderr(t *testing.T) {
	input := writeIngestFixture(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"-input", input, "-layout-id", "gt7_layout_1240", "-epsilon", "0.5"}, &stdout, &stderr); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if !strings.Contains(stderr.String(), "closing report") {
		t.Fatalf("expected report on stderr, got %q", stderr.String())
	}
	for _, want := range []string{"layoutId:", "source point count:", "source segment count:", "source segment length meters:", "source planar heading change (X/Z):", "source path length meters:", "generated point count:", "generated path length meters:", "catalogLengthMeters:", "deltaMeters:", "deltaPct:"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("expected %q in stderr report, got %q", want, stderr.String())
		}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("stdout should be JSON: %v", err)
	}
	if _, ok := raw["report"]; ok {
		t.Fatal("report leaked into catalog JSON")
	}
	var catalog tracks.Catalog
	if err := json.Unmarshal(stdout.Bytes(), &catalog); err != nil {
		t.Fatalf("stdout should unmarshal as catalog: %v", err)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("catalog should validate structurally: %v", err)
	}
}

func TestRunRejectsInvalidArgumentsAndMissingLayout(t *testing.T) {
	input := writeIngestFixture(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"-input", input, "-layout-id", "", "-smoothing-window", "2"}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "layout-id") {
		t.Fatalf("expected invalid args error, got %v", err)
	}
	if err := run([]string{"-input", input, "-layout-id", "missing-layout"}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "requested layout") {
		t.Fatalf("expected missing layout error, got %v", err)
	}
}

func TestRunCanWriteCatalogToOutputFile(t *testing.T) {
	input := writeIngestFixture(t)
	output := filepath.Join(t.TempDir(), "catalog.json")
	if err := run([]string{"-input", input, "-layout-id", "gt7_layout_1240", "-output", output}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var catalog tracks.Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatalf("output should be JSON: %v", err)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("output should validate structurally: %v", err)
	}
}

func TestRunWritesTelemetryBaselineJSONToStdoutAndReportToStderr(t *testing.T) {
	accepted := writeIngestFixture(t)
	rejected := writeIngestRequest(t, telemetry.IngestBatchRequest{SessionID: "trackbuilder-rejected", Frames: []telemetry.Frame{{TimestampUnixMs: 1, PositionX: 0, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 0, IsOnTrack: true}, {TimestampUnixMs: 2, PositionX: 15, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 1, IsOnTrack: true}, {TimestampUnixMs: 3, PositionX: 30, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 2, IsOnTrack: true}}})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"-baseline", "-input", accepted, "-input", rejected, "-layout-id", "gt7_layout_1240"}, &stdout, &stderr); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if !strings.Contains(stderr.String(), "trackbuilder observed telemetry baseline") {
		t.Fatalf("expected baseline report on stderr, got %q", stderr.String())
	}
	for _, want := range []string{"status: telemetry_baseline_only", "sampleCount: 2", "acceptedLapCount: 1", "rejectedLapCount: 1", "acceptanceRule:"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("expected %q in stderr report, got %q", want, stderr.String())
		}
	}
	var baseline trackbuilder.TelemetryBaselineReport
	if err := json.Unmarshal(stdout.Bytes(), &baseline); err != nil {
		t.Fatalf("stdout should be JSON: %v", err)
	}
	_, seedLayout, ok := findSeedLayoutFromCatalog(tracks.OfficialGT7SeedCatalog(), "gt7_layout_1240")
	if !ok {
		t.Fatal("expected seed layout to resolve")
	}
	if baseline.Status != "telemetry_baseline_only" || baseline.LayoutID != "gt7_layout_1240" || baseline.LayoutName != seedLayout.Name {
		t.Fatalf("unexpected baseline identity: %+v", baseline)
	}
	if baseline.SampleCount != 2 || baseline.AcceptedLapCount != 1 || baseline.RejectedLapCount != 1 {
		t.Fatalf("unexpected baseline counts: %+v", baseline)
	}
	if baseline.CatalogLengthMeters != seedLayout.LengthMeters {
		t.Fatalf("unexpected catalog length: got %.2f want %.2f", baseline.CatalogLengthMeters, seedLayout.LengthMeters)
	}
	if math.Abs(baseline.AvgSourcePathLengthMeters-40) > 1e-9 || math.Abs(baseline.MinSourcePathLengthMeters-40) > 1e-9 || math.Abs(baseline.MaxSourcePathLengthMeters-40) > 1e-9 || math.Abs(baseline.StdDevSourcePathLengthMeters) > 1e-9 {
		t.Fatalf("unexpected source path stats: %+v", baseline)
	}
	wantAvgDelta := 40 - seedLayout.LengthMeters
	wantAvgDeltaPct := ((40 / seedLayout.LengthMeters) - 1) * 100
	if math.Abs(baseline.AvgDeltaMeters-wantAvgDelta) > 1e-9 || math.Abs(baseline.AvgDeltaPct-wantAvgDeltaPct) > 1e-9 {
		t.Fatalf("unexpected baseline deltas: %+v", baseline)
	}
}

func TestRunBaselineReportsMalformedInputPath(t *testing.T) {
	good := writeIngestFixture(t)
	badDir := t.TempDir()
	bad := filepath.Join(badDir, "broken-baseline.json")
	if err := os.WriteFile(bad, []byte(`{"sessionId":`), 0o600); err != nil {
		t.Fatalf("write malformed fixture: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"-baseline", "-input", good, "-input", bad, "-layout-id", "gt7_layout_1240"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected malformed input error")
	}
	for _, want := range []string{"decode ingest batch", bad} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in error, got %v", want, err)
		}
	}
}

func writeIngestFixture(t *testing.T) string {
	t.Helper()
	return writeIngestRequest(t, telemetry.IngestBatchRequest{SessionID: "trackbuilder", Frames: []telemetry.Frame{{TimestampUnixMs: 1, PositionX: 0, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 0, IsOnTrack: true}, {TimestampUnixMs: 2, PositionX: 10, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 1, IsOnTrack: true}, {TimestampUnixMs: 3, PositionX: 10, PositionY: 10, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 2, IsOnTrack: true}, {TimestampUnixMs: 4, PositionX: 0, PositionY: 10, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 3, IsOnTrack: true}, {TimestampUnixMs: 5, PositionX: 0, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 4, IsOnTrack: true}}})
}

func writeIngestRequest(t *testing.T, fixture telemetry.IngestBatchRequest) string {
	t.Helper()
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "ingest.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	return path
}

func findSeedLayoutFromCatalog(catalog tracks.Catalog, layoutID string) (tracks.CatalogTrack, tracks.CatalogLayout, bool) {
	for _, track := range catalog.Tracks {
		for _, layout := range track.Layouts {
			if layout.ID == layoutID {
				return track, layout, true
			}
		}
	}

	return tracks.CatalogTrack{}, tracks.CatalogLayout{}, false
}
