package events

import (
	"fmt"
	"math"

	"telemetry-one-backend/internal/telemetry"
)

const frameRuleVersionV1 = "v1"

const (
	lapRegressionRuleID = "lap_time_regression.v1"
	offTrackStintRuleID = "off_track_stint.v1"
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
	if event, ok := lapTimeRegressionEvent(sessionID, frames, options); ok {
		events = append(events, event)
	}
	if event, ok := offTrackStintEvent(sessionID, frames, options); ok {
		events = append(events, event)
	}

	for _, event := range events {
		if err := event.Validate(); err != nil {
			return nil, err
		}
	}

	return events, nil
}

func lapTimeRegressionEvent(sessionID string, frames []telemetry.Frame, options FrameEventOptions) (EngineerEvent, bool) {
	seen := make(map[int]struct{})
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
		return baseFrameEvent(sessionID, completedLapNumber, frame.TimestampUnixMs, TypeLapTimeRegression, severity, lapRegressionRuleID, []MetricEvidence{
			{Name: "lastLapMs", Value: float64(*frame.LastLapMs), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleActual},
			{Name: "bestLapMs", Value: float64(*frame.BestLapMs), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleReference},
			{Name: "lapRegressionDeltaMs", Value: float64(delta), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleDelta},
			{Name: "lapRegressionThresholdMs", Value: float64(threshold), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleThreshold},
		}, timeRange(frame.TimestampUnixMs, frame.TimestampUnixMs)), true
	}

	return EngineerEvent{}, false
}

func offTrackStintEvent(sessionID string, frames []telemetry.Frame, options FrameEventOptions) (EngineerEvent, bool) {
	var (
		streakStart telemetry.Frame
		streakEnd   telemetry.Frame
		streakCount int
		active      bool
	)

	emit := func() (EngineerEvent, bool) {
		if !active || streakCount == 0 {
			return EngineerEvent{}, false
		}
		durationMs := streakEnd.TimestampUnixMs - streakStart.TimestampUnixMs
		if durationMs < options.OffTrackMinDurationMs {
			return EngineerEvent{}, false
		}

		severity := severityForInt64(durationMs, options.OffTrackMediumDurationMs, options.OffTrackHighDurationMs)
		lapNumber := streakStart.LapNumber
		if lapNumber < 0 {
			lapNumber = 0
		}
		return baseFrameEvent(sessionID, lapNumber, streakEnd.TimestampUnixMs, TypeOffTrackStint, severity, offTrackStintRuleID, []MetricEvidence{
			{Name: "offTrackDurationMs", Value: float64(durationMs), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleActual},
			{Name: "offTrackFrameCount", Value: float64(streakCount), Unit: "frames", Status: MetricStatusAvailable, Role: MetricRoleActual},
			{Name: "offTrackThresholdMs", Value: float64(options.OffTrackMinDurationMs), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleThreshold},
		}, timeRange(streakStart.TimestampUnixMs, streakEnd.TimestampUnixMs)), true
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

		if event, ok := emit(); ok {
			return event, true
		}
		active = false
		streakCount = 0
	}

	if event, ok := emit(); ok {
		return event, true
	}

	return EngineerEvent{}, false
}

func baseFrameEvent(sessionID string, lapNumber int, timestampUnixMs int64, eventType EventType, severity Severity, ruleID string, metrics []MetricEvidence, eventRange *TimeRange) EngineerEvent {
	return EngineerEvent{
		EventID:         sanitizeID(fmt.Sprintf("%s-lap-%d-%s", sessionID, lapNumber, eventType)),
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
