package laps

import (
	"testing"

	"telemetry-one-backend/internal/telemetry"
)

func TestExtractCompletedRequiresExplicitPositiveLastLapEvidence(t *testing.T) {
	zero := int64(0)
	negative := int64(-1)
	positive := int64(91234)

	frames := []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 3, LastLapMs: nil},
		{TimestampUnixMs: 2000, LapNumber: 3, LastLapMs: &zero},
		{TimestampUnixMs: 3000, LapNumber: 3, LastLapMs: &negative},
		{TimestampUnixMs: 4000, LapNumber: 0, LastLapMs: &positive},
	}

	if got := ExtractCompleted("session-1", frames); len(got) != 0 {
		t.Fatalf("expected no completed laps, got %+v", got)
	}
}

func TestExtractCompletedUsesPreviousLapAndCarriesContext(t *testing.T) {
	lastLapMs := int64(91234)
	bestLapMs := int64(89000)

	got := ExtractCompleted("session-1", []telemetry.Frame{{
		TimestampUnixMs: 1720656000000,
		LapNumber:       3,
		LastLapMs:       &lastLapMs,
		BestLapMs:       &bestLapMs,
	}})

	if len(got) != 1 {
		t.Fatalf("expected one completed lap, got %+v", got)
	}
	lap := got[0]
	if lap.ID != "lap_session-1_2" || lap.SessionID != "session-1" || lap.LapNumber != 2 || lap.LapTimeMs != lastLapMs || lap.CompletedAtUnixMs != 1720656000000 {
		t.Fatalf("unexpected completed lap: %+v", lap)
	}
	if lap.BestLapMs == nil || *lap.BestLapMs != bestLapMs {
		t.Fatalf("expected best lap context %d, got %+v", bestLapMs, lap.BestLapMs)
	}
}

func TestExtractCompletedCarriesZeroBestLapContext(t *testing.T) {
	lastLapMs := int64(91234)
	bestLapMs := int64(0)

	got := ExtractCompleted("session-1", []telemetry.Frame{{
		TimestampUnixMs: 1720656000000,
		LapNumber:       3,
		LastLapMs:       &lastLapMs,
		BestLapMs:       &bestLapMs,
	}})

	if len(got) != 1 {
		t.Fatalf("expected one completed lap, got %+v", got)
	}
	if got[0].BestLapMs == nil || *got[0].BestLapMs != 0 {
		t.Fatalf("expected zero best lap context to be preserved, got %+v", got[0].BestLapMs)
	}
}

func TestExtractCompletedKeepsFirstEvidencePerLap(t *testing.T) {
	first := int64(90000)
	duplicate := int64(91000)

	got := ExtractCompleted("session-1", []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 2, LastLapMs: &first},
		{TimestampUnixMs: 2000, LapNumber: 2, LastLapMs: &duplicate},
	})

	if len(got) != 1 {
		t.Fatalf("expected duplicate evidence to collapse to one lap, got %+v", got)
	}
	if got[0].LapNumber != 1 || got[0].LapTimeMs != first || got[0].CompletedAtUnixMs != 1000 {
		t.Fatalf("expected first evidence to win, got %+v", got[0])
	}
}
