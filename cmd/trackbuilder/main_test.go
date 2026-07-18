package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"telemetry-one-backend/internal/telemetry"
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

func writeIngestFixture(t *testing.T) string {
	t.Helper()
	fixture := telemetry.IngestBatchRequest{SessionID: "trackbuilder", Frames: []telemetry.Frame{{TimestampUnixMs: 1, PositionX: 0, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 0, IsOnTrack: true}, {TimestampUnixMs: 2, PositionX: 10, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 1, IsOnTrack: true}, {TimestampUnixMs: 3, PositionX: 10, PositionY: 10, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 2, IsOnTrack: true}, {TimestampUnixMs: 4, PositionX: 0, PositionY: 10, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 3, IsOnTrack: true}, {TimestampUnixMs: 5, PositionX: 0, PositionY: 0, PositionZ: 0, SpeedMps: 1, RPM: 1, LapNumber: 1, CurrentLapMs: 4, IsOnTrack: true}}}
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
