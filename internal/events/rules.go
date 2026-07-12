package events

import (
	"fmt"
	"math"
	"strings"

	"telemetry-one-backend/internal/corners"
)

const ruleVersionV1 = "v1"

type RuleInput struct {
	SessionID       string
	LapNumber       int
	TimestampUnixMs int64
	CornerAnalysis  corners.Analysis
	Reference       *ReferenceMetrics
	Track           *CatalogRef
	Layout          *CatalogRef
	Corner          *CatalogRef
	Options         RuleOptions
}

type ReferenceMetrics struct {
	ExitSpeedKph ScalarReference
}

type ScalarReference struct {
	Available bool
	Value     float64
}

type RuleOptions struct {
	LateThrottleDelayLowMs     int64
	LateThrottleDelayMediumMs  int64
	LateThrottleDelayHighMs    int64
	LowExitSpeedDeltaLowKph    float64
	LowExitSpeedDeltaMediumKph float64
	LowExitSpeedDeltaHighKph   float64
}

func DefaultRuleOptions() RuleOptions {
	return RuleOptions{
		LateThrottleDelayLowMs:     250,
		LateThrottleDelayMediumMs:  500,
		LateThrottleDelayHighMs:    750,
		LowExitSpeedDeltaLowKph:    5,
		LowExitSpeedDeltaMediumKph: 10,
		LowExitSpeedDeltaHighKph:   15,
	}
}

func Generate(input RuleInput) ([]EngineerEvent, error) {
	if input.CornerAnalysis.Status == corners.AnalysisStatusUnavailable {
		return nil, nil
	}
	options := normalizeRuleOptions(input.Options)
	events := make([]EngineerEvent, 0, 2)

	if event, ok := lateThrottleEvent(input, options); ok {
		events = append(events, event)
	}
	if event, ok := lowExitSpeedEvent(input, options); ok {
		events = append(events, event)
	}

	for _, event := range events {
		if err := event.Validate(); err != nil {
			return nil, err
		}
	}

	return events, nil
}

func lateThrottleEvent(input RuleInput, options RuleOptions) (EngineerEvent, bool) {
	metric := input.CornerAnalysis.FirstThrottleReapplication
	if metric.Status != corners.MetricStatusAvailable || metric.TimeOffsetMs < options.LateThrottleDelayLowMs || input.TimestampUnixMs <= 0 {
		return EngineerEvent{}, false
	}

	severity := severityForInt64(metric.TimeOffsetMs, options.LateThrottleDelayMediumMs, options.LateThrottleDelayHighMs)
	startUnixMs := metric.TimestampUnixMs - metric.TimeOffsetMs
	return baseEvent(input, TypeLateThrottle, severity, "late_throttle.v1", []MetricEvidence{
		{Name: "firstThrottleReapplicationOffsetMs", Value: float64(metric.TimeOffsetMs), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleActual},
		{Name: "lateThrottleDelayThresholdMs", Value: float64(options.LateThrottleDelayLowMs), Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleThreshold},
		{Name: "firstThrottleReapplicationDistanceMeters", Value: metric.DistanceFromStart, Unit: "m", Status: MetricStatusAvailable, Role: MetricRoleActual},
	}, timeRange(startUnixMs, metric.TimestampUnixMs)), true
}

func lowExitSpeedEvent(input RuleInput, options RuleOptions) (EngineerEvent, bool) {
	if input.Reference == nil || !input.Reference.ExitSpeedKph.Available || !finite(input.Reference.ExitSpeedKph.Value) {
		return EngineerEvent{}, false
	}
	actual := input.CornerAnalysis.ExitSpeedKph
	if actual.Status != corners.MetricStatusAvailable || !finite(actual.Value) || input.TimestampUnixMs <= 0 {
		return EngineerEvent{}, false
	}

	delta := input.Reference.ExitSpeedKph.Value - actual.Value
	if delta < options.LowExitSpeedDeltaLowKph {
		return EngineerEvent{}, false
	}

	severity := severityForFloat64(delta, options.LowExitSpeedDeltaMediumKph, options.LowExitSpeedDeltaHighKph)
	return baseEvent(input, TypeLowExitSpeed, severity, "low_exit_speed.v1", []MetricEvidence{
		{Name: "exitSpeedKph", Value: actual.Value, Unit: "kph", Status: MetricStatusAvailable, Role: MetricRoleActual},
		{Name: "referenceExitSpeedKph", Value: input.Reference.ExitSpeedKph.Value, Unit: "kph", Status: MetricStatusAvailable, Role: MetricRoleReference},
		{Name: "exitSpeedDeltaKph", Value: delta, Unit: "kph", Status: MetricStatusAvailable, Role: MetricRoleDelta},
		{Name: "lowExitSpeedDeltaThresholdKph", Value: options.LowExitSpeedDeltaLowKph, Unit: "kph", Status: MetricStatusAvailable, Role: MetricRoleThreshold},
	}, timeRange(input.TimestampUnixMs, input.TimestampUnixMs)), true
}

func baseEvent(input RuleInput, eventType EventType, severity Severity, ruleID string, metrics []MetricEvidence, eventRange *TimeRange) EngineerEvent {
	return EngineerEvent{
		EventID:         eventID(input, eventType),
		SessionID:       input.SessionID,
		Version:         ContractVersionV1,
		Type:            eventType,
		Severity:        severity,
		Confidence:      confidenceForSeverity(severity),
		TimestampUnixMs: input.TimestampUnixMs,
		TimeRange:       eventRange,
		LapNumber:       input.LapNumber,
		Track:           input.Track,
		Layout:          input.Layout,
		Corner:          input.Corner,
		Metrics:         metrics,
		Source:          EventSource{Kind: SourceDeterministicRule, RuleID: ruleID, RuleVersion: ruleVersionV1},
	}
}

func eventID(input RuleInput, eventType EventType) string {
	cornerID := "unknown-corner"
	if input.CornerAnalysis.CornerID != "" {
		cornerID = input.CornerAnalysis.CornerID
	}
	return sanitizeID(fmt.Sprintf("%s-lap-%d-%s-%s", input.SessionID, input.LapNumber, cornerID, eventType))
}

func sanitizeID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "engineer-event"
	}
	var builder strings.Builder
	previousDash := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if valid {
			builder.WriteRune(r)
			previousDash = false
			continue
		}
		if !previousDash {
			builder.WriteByte('-')
			previousDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func timeRange(startUnixMs, endUnixMs int64) *TimeRange {
	if startUnixMs <= 0 || endUnixMs <= 0 || startUnixMs > endUnixMs {
		return nil
	}
	return &TimeRange{StartUnixMs: startUnixMs, EndUnixMs: endUnixMs}
}

func severityForInt64(value, medium, high int64) Severity {
	if value >= high {
		return SeverityHigh
	}
	if value >= medium {
		return SeverityMedium
	}
	return SeverityLow
}

func severityForFloat64(value, medium, high float64) Severity {
	if value >= high {
		return SeverityHigh
	}
	if value >= medium {
		return SeverityMedium
	}
	return SeverityLow
}

func confidenceForSeverity(severity Severity) float64 {
	switch severity {
	case SeverityHigh:
		return 0.9
	case SeverityMedium:
		return 0.82
	default:
		return 0.75
	}
}

func normalizeRuleOptions(options RuleOptions) RuleOptions {
	defaults := DefaultRuleOptions()
	if options.LateThrottleDelayLowMs <= 0 {
		options.LateThrottleDelayLowMs = defaults.LateThrottleDelayLowMs
	}
	if options.LateThrottleDelayMediumMs <= options.LateThrottleDelayLowMs {
		options.LateThrottleDelayMediumMs = defaults.LateThrottleDelayMediumMs
	}
	if options.LateThrottleDelayHighMs <= options.LateThrottleDelayMediumMs {
		options.LateThrottleDelayHighMs = defaults.LateThrottleDelayHighMs
	}
	if options.LowExitSpeedDeltaLowKph <= 0 || math.IsNaN(options.LowExitSpeedDeltaLowKph) || math.IsInf(options.LowExitSpeedDeltaLowKph, 0) {
		options.LowExitSpeedDeltaLowKph = defaults.LowExitSpeedDeltaLowKph
	}
	if options.LowExitSpeedDeltaMediumKph <= options.LowExitSpeedDeltaLowKph || math.IsNaN(options.LowExitSpeedDeltaMediumKph) || math.IsInf(options.LowExitSpeedDeltaMediumKph, 0) {
		options.LowExitSpeedDeltaMediumKph = defaults.LowExitSpeedDeltaMediumKph
	}
	if options.LowExitSpeedDeltaHighKph <= options.LowExitSpeedDeltaMediumKph || math.IsNaN(options.LowExitSpeedDeltaHighKph) || math.IsInf(options.LowExitSpeedDeltaHighKph, 0) {
		options.LowExitSpeedDeltaHighKph = defaults.LowExitSpeedDeltaHighKph
	}
	return options
}
