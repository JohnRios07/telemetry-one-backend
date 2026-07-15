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
	if event.EventID != "session-1-lap-1-lap_time_regression-1100-1100" {
		t.Fatalf("expected deterministic event id, got %q", event.EventID)
	}
	if event.Source.RuleID != lapRegressionRuleID || event.Source.RuleVersion != frameRuleVersionV1 {
		t.Fatalf("expected deterministic source metadata, got %+v", event.Source)
	}
	if DedupKey(event) != "session-1|lap_time_regression|lap:1|track:<none>|layout:<none>|corner:<none>|source_kind:deterministic_rule|rule_id:lap_time_regression.v1|rule_version:v1|time_range:1100-1100" {
		t.Fatalf("expected deterministic dedup key, got %q", DedupKey(event))
	}
}

func TestGenerateFrameEventsLapTimeRegressionEmitsAllOccurrences(t *testing.T) {
	events, err := GenerateFrameEvents("session-1", []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 2, CurrentLapMs: 100, LastLapMs: ptrInt64(94500), BestLapMs: ptrInt64(90000), IsOnTrack: true},
		{TimestampUnixMs: 1100, LapNumber: 3, CurrentLapMs: 200, LastLapMs: ptrInt64(94500), BestLapMs: ptrInt64(90000), IsOnTrack: true},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %+v", events)
	}
	if events[0].EventID == events[1].EventID {
		t.Fatalf("expected distinct event ids, got %+v", events)
	}
	if DedupKey(events[0]) == DedupKey(events[1]) {
		t.Fatalf("expected distinct dedup keys, got %+v", events)
	}
	if events[0].EventID != "session-1-lap-1-lap_time_regression-1000-1000" || events[1].EventID != "session-1-lap-2-lap_time_regression-1100-1100" {
		t.Fatalf("expected stable event ids with time ranges, got %+v", events)
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
	if event.EventID != "session-1-lap-1-off_track_stint-1200-1800" {
		t.Fatalf("expected deterministic event id, got %q", event.EventID)
	}
}

func TestGenerateFrameEventsOffTrackStintEmitsAllOccurrences(t *testing.T) {
	events, err := GenerateFrameEvents("session-1", []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 1, CurrentLapMs: 1000, IsOnTrack: true},
		{TimestampUnixMs: 1200, LapNumber: 1, CurrentLapMs: 1200, IsOnTrack: false},
		{TimestampUnixMs: 1500, LapNumber: 1, CurrentLapMs: 1500, IsOnTrack: false},
		{TimestampUnixMs: 1800, LapNumber: 1, CurrentLapMs: 1800, IsOnTrack: false},
		{TimestampUnixMs: 1900, LapNumber: 1, CurrentLapMs: 1900, IsOnTrack: true},
		{TimestampUnixMs: 2100, LapNumber: 1, CurrentLapMs: 2100, IsOnTrack: false},
		{TimestampUnixMs: 2400, LapNumber: 1, CurrentLapMs: 2400, IsOnTrack: false},
		{TimestampUnixMs: 2700, LapNumber: 1, CurrentLapMs: 2700, IsOnTrack: false},
		{TimestampUnixMs: 2800, LapNumber: 1, CurrentLapMs: 2800, IsOnTrack: true},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %+v", events)
	}
	if events[0].EventID == events[1].EventID {
		t.Fatalf("expected distinct event ids, got %+v", events)
	}
	if DedupKey(events[0]) == DedupKey(events[1]) {
		t.Fatalf("expected distinct dedup keys, got %+v", events)
	}
	if events[0].EventID != "session-1-lap-1-off_track_stint-1200-1800" || events[1].EventID != "session-1-lap-1-off_track_stint-2100-2700" {
		t.Fatalf("expected stable event ids with time ranges, got %+v", events)
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

func TestGenerateFrameEventsForAppendPreviousLapRegression(t *testing.T) {
	tests := []struct {
		name            string
		previousLapMs   int64
		completedLapMs  int64
		wantEvent       bool
		wantThresholdMs float64
		wantDeltaMs     float64
	}{
		{name: "emits at three percent threshold", previousLapMs: 100000, completedLapMs: 103000, wantEvent: true, wantThresholdMs: 3000, wantDeltaMs: 3000},
		{name: "emits at floor threshold", previousLapMs: 40000, completedLapMs: 41500, wantEvent: true, wantThresholdMs: 1500, wantDeltaMs: 1500},
		{name: "does not emit below threshold", previousLapMs: 100000, completedLapMs: 102999, wantEvent: false},
		{name: "does not emit for equal lap", previousLapMs: 100000, completedLapMs: 100000, wantEvent: false},
		{name: "does not emit for faster lap", previousLapMs: 100000, completedLapMs: 98000, wantEvent: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accumulated := []telemetry.Frame{
				{TimestampUnixMs: 1000, LapNumber: 5, CurrentLapMs: 100, LastLapMs: ptrInt64(tt.previousLapMs), BestLapMs: ptrInt64(tt.previousLapMs), IsOnTrack: true},
				{TimestampUnixMs: 2000, LapNumber: 6, CurrentLapMs: 100, LastLapMs: ptrInt64(tt.completedLapMs), BestLapMs: ptrInt64(tt.completedLapMs), IsOnTrack: true},
			}
			appended := accumulated[1:]

			events, err := GenerateFrameEventsForAppend("session-1", accumulated, appended)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if !tt.wantEvent {
				if len(events) != 0 {
					t.Fatalf("expected no events, got %+v", events)
				}
				return
			}

			if len(events) != 1 {
				t.Fatalf("expected 1 event, got %+v", events)
			}
			event := events[0]
			if event.Type != TypeLapTimeRegression || event.Source.RuleID != previousLapRegressionRuleID {
				t.Fatalf("expected previous-lap regression event, got %+v", event)
			}
			if event.LapNumber != 5 {
				t.Fatalf("expected completed lap 5, got %d", event.LapNumber)
			}
			if event.TimeRange != nil {
				t.Fatalf("expected previous-lap regression identity not to depend on evidence time range, got %+v", event.TimeRange)
			}
			assertFrameMetric(t, event, "completedLapMs", float64(tt.completedLapMs))
			assertFrameMetric(t, event, "previousCompletedLapMs", float64(tt.previousLapMs))
			assertFrameMetric(t, event, "lapPaceDropDeltaMs", tt.wantDeltaMs)
			assertFrameMetric(t, event, "lapPaceDropThresholdMs", tt.wantThresholdMs)
		})
	}
}

func TestGenerateFrameEventsForAppendPreviousLapRegressionDeduplicatesRepeatedEvidence(t *testing.T) {
	accumulated := []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 2, CurrentLapMs: 100, LastLapMs: ptrInt64(40000), IsOnTrack: true},
		{TimestampUnixMs: 2000, LapNumber: 3, CurrentLapMs: 100, LastLapMs: ptrInt64(41500), IsOnTrack: true},
		{TimestampUnixMs: 2100, LapNumber: 3, CurrentLapMs: 200, LastLapMs: ptrInt64(41500), IsOnTrack: true},
	}
	appended := accumulated[1:]

	events, err := GenerateFrameEventsForAppend("session-1", accumulated, appended)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one deduped previous-lap event, got %+v", events)
	}
	if events[0].Source.RuleID != previousLapRegressionRuleID || events[0].EventID != "session-1-lap-2-lap_time_regression-lap_time_regression-previous_lap-v1" {
		t.Fatalf("expected stable previous-lap event, got %+v", events[0])
	}
}

func TestGenerateFrameEventsForAppendPreviousLapRegressionUsesStableIdentityAcrossRepeatedEvidence(t *testing.T) {
	firstAccumulated := []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 2, CurrentLapMs: 100, LastLapMs: ptrInt64(40000), IsOnTrack: true},
		{TimestampUnixMs: 2000, LapNumber: 3, CurrentLapMs: 100, LastLapMs: ptrInt64(41500), IsOnTrack: true},
	}
	secondAccumulated := append([]telemetry.Frame{}, firstAccumulated...)
	secondAccumulated = append(secondAccumulated, telemetry.Frame{TimestampUnixMs: 2500, LapNumber: 3, CurrentLapMs: 600, LastLapMs: ptrInt64(41500), IsOnTrack: true})

	firstEvents, err := GenerateFrameEventsForAppend("session-1", firstAccumulated, firstAccumulated[1:])
	if err != nil {
		t.Fatalf("expected no error from first generation, got %v", err)
	}
	secondEvents, err := GenerateFrameEventsForAppend("session-1", secondAccumulated, secondAccumulated[2:])
	if err != nil {
		t.Fatalf("expected no error from second generation, got %v", err)
	}

	if len(firstEvents) != 1 || len(secondEvents) != 1 {
		t.Fatalf("expected one previous-lap event from each generation, got first=%+v second=%+v", firstEvents, secondEvents)
	}
	if firstEvents[0].TimestampUnixMs == secondEvents[0].TimestampUnixMs {
		t.Fatalf("test setup should use distinct evidence timestamps, got %d", firstEvents[0].TimestampUnixMs)
	}
	if firstEvents[0].EventID != secondEvents[0].EventID {
		t.Fatalf("expected repeated evidence for same completed lap to keep event id stable, got %q and %q", firstEvents[0].EventID, secondEvents[0].EventID)
	}
	if DedupKey(firstEvents[0]) != DedupKey(secondEvents[0]) {
		t.Fatalf("expected repeated evidence for same completed lap to keep dedup key stable, got %q and %q", DedupKey(firstEvents[0]), DedupKey(secondEvents[0]))
	}
}

func TestGenerateFrameEventsForAppendPreviousLapRegressionRequiresAppendedBoundary(t *testing.T) {
	accumulated := []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 8, CurrentLapMs: 100, LastLapMs: ptrInt64(100000), IsOnTrack: true},
		{TimestampUnixMs: 2000, LapNumber: 9, CurrentLapMs: 100, LastLapMs: ptrInt64(103000), IsOnTrack: true},
	}
	appended := []telemetry.Frame{{TimestampUnixMs: 2500, LapNumber: 9, CurrentLapMs: 500, IsOnTrack: true}}

	events, err := GenerateFrameEventsForAppend("session-1", accumulated, appended)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected historical regression not to re-emit without appended boundary, got %+v", events)
	}
}

func TestGenerateFrameEventsForAppendOffTrackStreakAcrossBatches(t *testing.T) {
	accumulated := []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 1, CurrentLapMs: 1000, IsOnTrack: true},
		{TimestampUnixMs: 1200, LapNumber: 1, CurrentLapMs: 1200, IsOnTrack: false},
		{TimestampUnixMs: 1500, LapNumber: 1, CurrentLapMs: 1500, IsOnTrack: false},
	}
	appended := []telemetry.Frame{
		{TimestampUnixMs: 1800, LapNumber: 1, CurrentLapMs: 1800, IsOnTrack: false},
		{TimestampUnixMs: 2000, LapNumber: 1, CurrentLapMs: 2000, IsOnTrack: true},
	}

	events, err := GenerateFrameEventsForAppend("session-1", accumulated, appended)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 off-track event, got %d: %+v", len(events), events)
	}
	event := events[0]
	if event.Type != TypeOffTrackStint {
		t.Fatalf("expected off_track_stint event, got %s", event.Type)
	}
	if event.TimeRange == nil || event.TimeRange.StartUnixMs != 1200 || event.TimeRange.EndUnixMs != 1800 {
		t.Fatalf("expected time range 1200-1800 covering full off-track streak, got %+v", event.TimeRange)
	}
	if event.LapNumber != 1 {
		t.Fatalf("expected lap 1, got %d", event.LapNumber)
	}
}

func TestGenerateFrameEventsForAppendPreservesBestLapAndOffTrackBehavior(t *testing.T) {
	accumulated := []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 2, CurrentLapMs: 100, LastLapMs: ptrInt64(94500), BestLapMs: ptrInt64(90000), IsOnTrack: false},
		{TimestampUnixMs: 1600, LapNumber: 2, CurrentLapMs: 700, IsOnTrack: false},
	}

	events, err := GenerateFrameEventsForAppend("session-1", accumulated, accumulated)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected best-lap regression and off-track events, got %+v", events)
	}
	if events[0].Source.RuleID != lapRegressionRuleID || events[1].Source.RuleID != offTrackStintRuleID {
		t.Fatalf("expected existing rules to remain unchanged, got %+v", events)
	}
}

func TestGenerateFrameEventsForAppendEmitsBothBestLapAndPreviousLapRegression(t *testing.T) {
	accumulated := []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 2, CurrentLapMs: 100, LastLapMs: ptrInt64(40000), BestLapMs: ptrInt64(40000), IsOnTrack: true},
	}
	appended := []telemetry.Frame{
		{TimestampUnixMs: 2000, LapNumber: 3, CurrentLapMs: 100, LastLapMs: ptrInt64(42000), BestLapMs: ptrInt64(40000), IsOnTrack: true},
	}
	fullAccumulated := append(accumulated, appended...)

	events, err := GenerateFrameEventsForAppend("session-1", fullAccumulated, appended)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events (best-lap + previous-lap), got %+v", events)
	}

	var bestLapEvent, previousLapEvent *EngineerEvent
	for i := range events {
		switch events[i].Source.RuleID {
		case lapRegressionRuleID:
			bestLapEvent = &events[i]
		case previousLapRegressionRuleID:
			previousLapEvent = &events[i]
		}
	}
	if bestLapEvent == nil {
		t.Fatal("expected best-lap regression event")
	}
	if previousLapEvent == nil {
		t.Fatal("expected previous-lap regression event")
	}

	if bestLapEvent.LapNumber != 2 || previousLapEvent.LapNumber != 2 {
		t.Fatalf("both events should reference completed lap 2, got best-lap lap=%d previous-lap lap=%d", bestLapEvent.LapNumber, previousLapEvent.LapNumber)
	}
	if DedupKey(*bestLapEvent) == DedupKey(*previousLapEvent) {
		t.Fatalf("expected distinct dedup keys for best-lap and previous-lap events")
	}
	assertFrameMetric(t, *bestLapEvent, "lastLapMs", 42000)
	assertFrameMetric(t, *bestLapEvent, "bestLapMs", 40000)
	assertFrameMetric(t, *previousLapEvent, "completedLapMs", 42000)
	assertFrameMetric(t, *previousLapEvent, "previousCompletedLapMs", 40000)
}

func ptrInt64(value int64) *int64 {
	return &value
}

func assertFrameMetric(t *testing.T, event EngineerEvent, name string, want float64) {
	t.Helper()
	for _, metric := range event.Metrics {
		if metric.Name == name {
			if metric.Value != want {
				t.Fatalf("metric %s = %v, want %v", name, metric.Value, want)
			}
			return
		}
	}
	t.Fatalf("metric %s not found in %+v", name, event.Metrics)
}
