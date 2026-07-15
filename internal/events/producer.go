package events

import (
	"fmt"
	"math"

	"telemetry-one-backend/internal/telemetry"
)

const frameRuleVersionV1 = "v1"

const (
	lapRegressionRuleID             = "lap_time_regression.v1"
	previousLapRegressionRuleID     = "lap_time_regression.previous_lap.v1"
	previousLapRegressionMinDeltaMs = int64(1500)
	previousLapRegressionDeltaPct   = 0.03
	offTrackStintRuleID             = "off_track_stint.v1"
)

type FrameEventOptions struct {
	LapRegressionMinDeltaMs  int64
	LapRegressionDeltaPct    float64
	OffTrackMinDurationMs    int64
	OffTrackLowDurationMs    int64
	OffTrackMediumDurationMs int64
	OffTrackHighDurationMs   int64
}

func DefaultFrameEventOptions() FrameEventOptions {
	return FrameEventOptions{
		LapRegressionMinDeltaMs:  1500,
		LapRegressionDeltaPct:    0.03,
		OffTrackMinDurationMs:    500,
		OffTrackLowDurationMs:    500,
		OffTrackMediumDurationMs: 1500,
		OffTrackHighDurationMs:   3000,
	}
}

func GenerateFrameEvents(sessionID string, frames []telemetry.Frame) ([]EngineerEvent, error) {
	return GenerateFrameEventsWithOptions(sessionID, frames, DefaultFrameEventOptions())
}

func GenerateFrameEventsWithOptions(sessionID string, frames []telemetry.Frame, options FrameEventOptions) ([]EngineerEvent, error) {
	if sessionID == "" {
		return nil, ErrMissingSessionID
	}

	options = normalizeFrameEventOptions(options)
	events := make([]EngineerEvent, 0, 2)
	events = append(events, lapTimeRegressionEvents(sessionID, frames, options)...)
	events = append(events, offTrackStintEvents(sessionID, frames, options)...)

	for _, event := range events {
		if err := event.Validate(); err != nil {
			return nil, err
		}
	}

	return events, nil
}

func GenerateFrameEventsForAppend(sessionID string, accumulatedFrames []telemetry.Frame, appendedFrames []telemetry.Frame) ([]EngineerEvent, error) {
	if sessionID == "" {
		return nil, ErrMissingSessionID
	}

	options := DefaultFrameEventOptions()
	events := make([]EngineerEvent, 0, 2)
	bestLapRegressionEvents := lapTimeRegressionEvents(sessionID, appendedFrames, options)
	events = append(events, bestLapRegressionEvents...)
	events = append(events, previousLapRegressionEvents(sessionID, accumulatedFrames, appendedFrames, completedLapEventSet(bestLapRegressionEvents))...)
	events = append(events, offTrackStintEvents(sessionID, appendedFrames, options)...)

	for _, event := range events {
		if err := event.Validate(); err != nil {
			return nil, err
		}
	}

	return events, nil
}

func lapTimeRegressionEvents(sessionID string, frames []telemetry.Frame, options FrameEventOptions) []EngineerEvent {
	seen := make(map[int]struct{})
	events := make([]EngineerEvent, 0, len(frames))
	for _, frame := range frames {
		if frame.LapNumber <= 0 || frame.LastLapMs == nil || frame.BestLapMs == nil || *frame.LastLapMs <= 0 || *frame.BestLapMs <= 0 {
			continue
		}

		completedLapNumber := frame.LapNumber - 1
		if completedLapNumber < 0 {
			continue
		}
		if _, ok := seen[completedLapNumber]; ok {
			continue
		}

		threshold := options.LapRegressionMinDeltaMs
		if ratioThreshold := int64(math.Ceil(float64(*frame.BestLapMs) * options.LapRegressionDeltaPct)); ratioThreshold > threshold {
			threshold = ratioThreshold
		}

		delta := *frame.LastLapMs - *frame.BestLapMs
		if delta < threshold {
			continue
		}

		seen[completedLapNumber] = struct{}{}
		severity := severityForInt64(delta, threshold*2, threshold*3)
		events = append(events, baseFrameEvent(sessionID, completedLapNumber, frame.TimestampUnixMs, TypeLapTimeRegression, severity, lapRegressionRuleID, []MetricEvidence{
			{Name: "lastLapMs", Value: float64(*frame.LastLapMs), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleActual},
			{Name: "bestLapMs", Value: float64(*frame.BestLapMs), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleReference},
			{Name: "lapRegressionDeltaMs", Value: float64(delta), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleDelta},
			{Name: "lapRegressionThresholdMs", Value: float64(threshold), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleThreshold},
		}, timeRange(frame.TimestampUnixMs, frame.TimestampUnixMs)))
	}

	return events
}

type lapCompletion struct {
	LapNumber       int
	CompletedLapMs  int64
	TimestampUnixMs int64
}

func completedLaps(frames []telemetry.Frame) []lapCompletion {
	seen := make(map[int]struct{}, len(frames))
	completions := make([]lapCompletion, 0, len(frames))
	for _, frame := range frames {
		if frame.LapNumber <= 0 || frame.LastLapMs == nil || *frame.LastLapMs <= 0 || frame.TimestampUnixMs <= 0 {
			continue
		}

		completedLapNumber := frame.LapNumber - 1
		if completedLapNumber < 0 {
			continue
		}
		if _, ok := seen[completedLapNumber]; ok {
			continue
		}

		seen[completedLapNumber] = struct{}{}
		completions = append(completions, lapCompletion{
			LapNumber:       completedLapNumber,
			CompletedLapMs:  *frame.LastLapMs,
			TimestampUnixMs: frame.TimestampUnixMs,
		})
	}

	return completions
}

func completedLapEventSet(events []EngineerEvent) map[int]struct{} {
	seen := make(map[int]struct{}, len(events))
	for _, event := range events {
		if event.Type == TypeLapTimeRegression {
			seen[event.LapNumber] = struct{}{}
		}
	}
	return seen
}

func previousLapRegressionEvents(sessionID string, accumulatedFrames []telemetry.Frame, appendedFrames []telemetry.Frame, skippedLaps map[int]struct{}) []EngineerEvent {
	appendedCompletions := completedLaps(appendedFrames)
	if len(appendedCompletions) == 0 {
		return nil
	}

	completionByLap := make(map[int]lapCompletion, len(accumulatedFrames))
	for _, completion := range completedLaps(accumulatedFrames) {
		completionByLap[completion.LapNumber] = completion
	}

	events := make([]EngineerEvent, 0, len(appendedCompletions))
	for _, completion := range appendedCompletions {
		if _, ok := skippedLaps[completion.LapNumber]; ok {
			continue
		}

		previousCompletion, ok := completionByLap[completion.LapNumber-1]
		if !ok || previousCompletion.CompletedLapMs <= 0 {
			continue
		}

		threshold := previousLapRegressionMinDeltaMs
		if ratioThreshold := int64(math.Ceil(float64(previousCompletion.CompletedLapMs) * previousLapRegressionDeltaPct)); ratioThreshold > threshold {
			threshold = ratioThreshold
		}

		delta := completion.CompletedLapMs - previousCompletion.CompletedLapMs
		if delta < threshold {
			continue
		}

		severity := severityForInt64(delta, threshold*2, threshold*3)
		events = append(events, basePreviousLapRegressionEvent(sessionID, completion.LapNumber, completion.TimestampUnixMs, severity, []MetricEvidence{
			{Name: "completedLapMs", Value: float64(completion.CompletedLapMs), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleActual},
			{Name: "previousCompletedLapMs", Value: float64(previousCompletion.CompletedLapMs), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleReference},
			{Name: "lapPaceDropDeltaMs", Value: float64(delta), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleDelta},
			{Name: "lapPaceDropThresholdMs", Value: float64(threshold), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleThreshold},
		}))
	}

	return events
}

func offTrackStintEvents(sessionID string, frames []telemetry.Frame, options FrameEventOptions) []EngineerEvent {
	var (
		streakStart telemetry.Frame
		streakEnd   telemetry.Frame
		streakCount int
		active      bool
	)
	events := make([]EngineerEvent, 0, len(frames))

	emit := func() {
		if !active || streakCount == 0 {
			return
		}
		durationMs := streakEnd.TimestampUnixMs - streakStart.TimestampUnixMs
		if durationMs < options.OffTrackMinDurationMs {
			return
		}

		severity := severityForInt64(durationMs, options.OffTrackMediumDurationMs, options.OffTrackHighDurationMs)
		lapNumber := streakStart.LapNumber
		if lapNumber < 0 {
			lapNumber = 0
		}
		events = append(events, baseFrameEvent(sessionID, lapNumber, streakEnd.TimestampUnixMs, TypeOffTrackStint, severity, offTrackStintRuleID, []MetricEvidence{
			{Name: "offTrackDurationMs", Value: float64(durationMs), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleActual},
			{Name: "offTrackFrameCount", Value: float64(streakCount), Unit: "frames", Status: MetricStatusAvailable, Role: MetricRoleActual},
			{Name: "offTrackThresholdMs", Value: float64(options.OffTrackMinDurationMs), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleThreshold},
		}, timeRange(streakStart.TimestampUnixMs, streakEnd.TimestampUnixMs)))
	}

	for _, frame := range frames {
		if !frame.IsOnTrack {
			if !active {
				active = true
				streakStart = frame
				streakCount = 0
			}
			streakEnd = frame
			streakCount++
			continue
		}

		emit()
		active = false
		streakCount = 0
	}

	emit()

	return events
}

func basePreviousLapRegressionEvent(sessionID string, lapNumber int, timestampUnixMs int64, severity Severity, metrics []MetricEvidence) EngineerEvent {
	event := baseFrameEvent(sessionID, lapNumber, timestampUnixMs, TypeLapTimeRegression, severity, previousLapRegressionRuleID, metrics, nil)
	event.EventID = frameRuleEventID(sessionID, lapNumber, TypeLapTimeRegression, previousLapRegressionRuleID)
	return event
}

func baseFrameEvent(sessionID string, lapNumber int, timestampUnixMs int64, eventType EventType, severity Severity, ruleID string, metrics []MetricEvidence, eventRange *TimeRange) EngineerEvent {
	return EngineerEvent{
		EventID:         frameEventID(sessionID, lapNumber, eventType, eventRange),
		SessionID:       sessionID,
		Version:         ContractVersionV1,
		Type:            eventType,
		Severity:        severity,
		Confidence:      confidenceForSeverity(severity),
		TimestampUnixMs: timestampUnixMs,
		TimeRange:       eventRange,
		LapNumber:       lapNumber,
		Metrics:         metrics,
		Source:          EventSource{Kind: SourceDeterministicRule, RuleID: ruleID, RuleVersion: frameRuleVersionV1},
	}
}

func frameEventID(sessionID string, lapNumber int, eventType EventType, eventRange *TimeRange) string {
	if eventRange != nil {
		return sanitizeID(fmt.Sprintf("%s-lap-%d-%s-%d-%d", sessionID, lapNumber, eventType, eventRange.StartUnixMs, eventRange.EndUnixMs))
	}
	return sanitizeID(fmt.Sprintf("%s-lap-%d-%s", sessionID, lapNumber, eventType))
}

func frameRuleEventID(sessionID string, lapNumber int, eventType EventType, ruleID string) string {
	return sanitizeID(fmt.Sprintf("%s-lap-%d-%s-%s", sessionID, lapNumber, eventType, ruleID))
}

func normalizeFrameEventOptions(options FrameEventOptions) FrameEventOptions {
	defaults := DefaultFrameEventOptions()
	if options.LapRegressionMinDeltaMs <= 0 {
		options.LapRegressionMinDeltaMs = defaults.LapRegressionMinDeltaMs
	}
	if options.LapRegressionDeltaPct <= 0 || math.IsNaN(options.LapRegressionDeltaPct) || math.IsInf(options.LapRegressionDeltaPct, 0) {
		options.LapRegressionDeltaPct = defaults.LapRegressionDeltaPct
	}
	if options.OffTrackMinDurationMs <= 0 {
		options.OffTrackMinDurationMs = defaults.OffTrackMinDurationMs
	}
	if options.OffTrackLowDurationMs <= 0 {
		options.OffTrackLowDurationMs = defaults.OffTrackLowDurationMs
	}
	if options.OffTrackMediumDurationMs <= options.OffTrackLowDurationMs {
		options.OffTrackMediumDurationMs = defaults.OffTrackMediumDurationMs
	}
	if options.OffTrackHighDurationMs <= options.OffTrackMediumDurationMs {
		options.OffTrackHighDurationMs = defaults.OffTrackHighDurationMs
	}
	return options
}
