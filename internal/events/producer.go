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
	events = append(events, lapTimeRegressionEvents(sessionID, frames, options)...)
	events = append(events, offTrackStintEvents(sessionID, frames, options)...)

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
