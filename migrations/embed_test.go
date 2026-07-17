package migrations

import (
	"strings"
	"testing"
)

func TestUpFilesIncludesRuntimePersistenceMigrationInOrder(t *testing.T) {
	files, err := UpFiles()
	if err != nil {
		t.Fatalf("expected up files: %v", err)
	}

	want := []string{"000001_initial_persistence.up.sql", "000002_sessions_lifecycle.up.sql", "000003_runtime_persistence.up.sql", "000004_detected_track_layout.up.sql", "000005_ingest_rejection_summaries.up.sql", "000006_persist_laps.up.sql", "000007_persist_lap_samples.up.sql", "000008_lap_telemetry_gap_count.up.sql"}
	if len(files) != len(want) {
		t.Fatalf("expected %d files, got %d: %v", len(want), len(files), files)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("expected file %d to be %q, got %q", i, want[i], files[i])
		}
	}
}

func TestTelemetryGapCountMigrationIsAdditiveWithNonNegativeDefault(t *testing.T) {
	contents, err := FS.ReadFile("000008_lap_telemetry_gap_count.up.sql")
	if err != nil {
		t.Fatalf("read telemetry gap migration: %v", err)
	}
	text := string(contents)
	for _, want := range []string{
		"ADD COLUMN IF NOT EXISTS telemetry_gap_count",
		"INTEGER NOT NULL DEFAULT 0",
		"CHECK (telemetry_gap_count >= 0)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected migration to contain %q, got %s", want, text)
		}
	}
}
