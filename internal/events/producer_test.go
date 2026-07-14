package events

import (
	"testing"

	"telemetry-one-backend/internal/telemetry"
)

func TestGenerateFrameEventsReturnsNoneForNormalFrames(t *testing.T) {
	events, err := GenerateFrameEvents("session-1", []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 1, CurrentLapMs: 1000, IsOnTrack: true},
		{TimestampUnixMs: 1100, LapNumber: 1, CurrentLapMs: 1100, IsOnTrack: true},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events, got %+v", events)
	}
}

func TestGenerateFrameEventsLapTimeRegressionThreshold(t *testing.T) {
	frames := []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 2, CurrentLapMs: 100, LastLapMs: ptrInt64(91000), BestLapMs: ptrInt64(90000), IsOnTrack: true},
		{TimestampUnixMs: 1100, LapNumber: 2, CurrentLapMs: 200, LastLapMs: ptrInt64(94500), BestLapMs: ptrInt64(90000), IsOnTrack: true},
	}

	events, err := GenerateFrameEvents("session-1", frames)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %+v", events)
	}
	event := events[0]
	if event.Type != TypeLapTimeRegression {
		t.Fatalf("expected lap time regression event, got %+v", event)
	}
	if event.EventID != "session-1-lap-1-lap_time_regression" {
		t.Fatalf("expected deterministic event id, got %q", event.EventID)
	}
	if event.Source.RuleID != lapRegressionRuleID || event.Source.RuleVersion != frameRuleVersionV1 {
		t.Fatalf("expected deterministic source metadata, got %+v", event.Source)
	}
	if DedupKey(event) != "session-1|lap_time_regression|lap:1|track:<none>|layout:<none>|corner:<none>|source_kind:deterministic_rule|rule_id:lap_time_regression.v1|rule_version:v1" {
		t.Fatalf("expected deterministic dedup key, got %q", DedupKey(event))
	}
}

func TestGenerateFrameEventsOffTrackStintRequiresSustainedDuration(t *testing.T) {
	frames := []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 1, CurrentLapMs: 1000, IsOnTrack: true},
		{TimestampUnixMs: 1200, LapNumber: 1, CurrentLapMs: 1200, IsOnTrack: false},
		{TimestampUnixMs: 1500, LapNumber: 1, CurrentLapMs: 1500, IsOnTrack: false},
		{TimestampUnixMs: 1800, LapNumber: 1, CurrentLapMs: 1800, IsOnTrack: false},
		{TimestampUnixMs: 1900, LapNumber: 1, CurrentLapMs: 1900, IsOnTrack: true},
	}

	events, err := GenerateFrameEvents("session-1", frames)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %+v", events)
	}
	event := events[0]
	if event.Type != TypeOffTrackStint {
		t.Fatalf("expected off-track stint event, got %+v", event)
	}
	if event.LapNumber != 1 {
		t.Fatalf("expected lap 1 event, got %d", event.LapNumber)
	}
	if event.TimestampUnixMs != 1800 {
		t.Fatalf("expected timestamp at end of stint, got %d", event.TimestampUnixMs)
	}
	if event.TimeRange == nil || event.TimeRange.StartUnixMs != 1200 || event.TimeRange.EndUnixMs != 1800 {
		t.Fatalf("expected time range to cover the off-track stint, got %+v", event.TimeRange)
	}
}

func TestGenerateFrameEventsOffTrackStintBelowThresholdIsIgnored(t *testing.T) {
	events, err := GenerateFrameEvents("session-1", []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 1, CurrentLapMs: 1000, IsOnTrack: true},
		{TimestampUnixMs: 1200, LapNumber: 1, CurrentLapMs: 1200, IsOnTrack: false},
		{TimestampUnixMs: 1400, LapNumber: 1, CurrentLapMs: 1400, IsOnTrack: true},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events for short off-track stint, got %+v", events)
	}
}

func ptrInt64(value int64) *int64 {
	return &value
}
