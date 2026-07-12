package events

import (
	"encoding/json"
	"strings"
	"testing"

	"telemetry-one-backend/internal/corners"
)

func TestGenerateReturnsNoEventsWhenMetricsUnavailable(t *testing.T) {
	events, err := Generate(ruleInput(func(input *RuleInput) {
		input.CornerAnalysis.FirstThrottleReapplication = corners.PointMetric{Status: corners.MetricStatusUnavailable, Reason: corners.ReasonMissingThrottleData}
		input.CornerAnalysis.ExitSpeedKph = corners.ScalarMetric{Status: corners.MetricStatusUnavailable, Reason: corners.ReasonNoSamples}
		input.Reference = &ReferenceMetrics{ExitSpeedKph: ScalarReference{Available: true, Value: 156}}
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events for unavailable metrics, got %+v", events)
	}
}

func TestGenerateCreatesLateThrottleEventWithDerivedEvidence(t *testing.T) {
	events, err := Generate(ruleInput(func(input *RuleInput) {
		input.CornerAnalysis.FirstThrottleReapplication.TimeOffsetMs = 520
		input.CornerAnalysis.FirstThrottleReapplication.TimestampUnixMs = 1520
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	event := requireEvent(t, events, TypeLateThrottle)

	if event.Version != ContractVersionV1 || event.Severity != SeverityMedium || event.Confidence != 0.82 {
		t.Fatalf("expected v1 medium late throttle event, got %+v", event)
	}
	if event.Source.Kind != SourceDeterministicRule || event.Source.RuleID != "late_throttle.v1" || event.Source.RuleVersion != "v1" {
		t.Fatalf("expected deterministic rule source, got %+v", event.Source)
	}
	if event.TimeRange == nil || event.TimeRange.StartUnixMs != 1000 || event.TimeRange.EndUnixMs != 1520 {
		t.Fatalf("expected throttle time range from corner entry to reapplication, got %+v", event.TimeRange)
	}
	assertMetric(t, event, "firstThrottleReapplicationOffsetMs", 520, "ms", MetricRoleActual)
	assertMetric(t, event, "lateThrottleDelayThresholdMs", 250, "ms", MetricRoleThreshold)
	assertMetric(t, event, "firstThrottleReapplicationDistanceMeters", 162, "m", MetricRoleActual)
}

func TestGenerateCreatesLowExitSpeedOnlyWhenBaselineExists(t *testing.T) {
	withoutBaseline, err := Generate(ruleInput(func(input *RuleInput) {
		input.CornerAnalysis.FirstThrottleReapplication.TimeOffsetMs = 100
		input.Reference = nil
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(withoutBaseline) != 0 {
		t.Fatalf("expected no low exit speed event without baseline, got %+v", withoutBaseline)
	}

	withBaseline, err := Generate(ruleInput(func(input *RuleInput) {
		input.CornerAnalysis.FirstThrottleReapplication.TimeOffsetMs = 100
		input.Reference = &ReferenceMetrics{ExitSpeedKph: ScalarReference{Available: true, Value: 158}}
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	event := requireEvent(t, withBaseline, TypeLowExitSpeed)
	if event.Severity != SeverityMedium {
		t.Fatalf("expected medium low exit speed event, got %+v", event)
	}
	assertMetric(t, event, "exitSpeedKph", 144, "kph", MetricRoleActual)
	assertMetric(t, event, "referenceExitSpeedKph", 158, "kph", MetricRoleReference)
	assertMetric(t, event, "exitSpeedDeltaKph", 14, "kph", MetricRoleDelta)
}

func TestGenerateSeverityThresholds(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*RuleInput)
		eventType EventType
		want      Severity
	}{
		{name: "late throttle low", eventType: TypeLateThrottle, want: SeverityLow, mutate: func(input *RuleInput) {
			input.CornerAnalysis.FirstThrottleReapplication.TimeOffsetMs = 300
			input.CornerAnalysis.FirstThrottleReapplication.TimestampUnixMs = 1300
			input.Reference = nil
		}},
		{name: "late throttle high", eventType: TypeLateThrottle, want: SeverityHigh, mutate: func(input *RuleInput) {
			input.CornerAnalysis.FirstThrottleReapplication.TimeOffsetMs = 800
			input.CornerAnalysis.FirstThrottleReapplication.TimestampUnixMs = 1800
			input.Reference = nil
		}},
		{name: "low exit speed low", eventType: TypeLowExitSpeed, want: SeverityLow, mutate: func(input *RuleInput) {
			input.CornerAnalysis.FirstThrottleReapplication.TimeOffsetMs = 100
			input.Reference = &ReferenceMetrics{ExitSpeedKph: ScalarReference{Available: true, Value: 150}}
		}},
		{name: "low exit speed high", eventType: TypeLowExitSpeed, want: SeverityHigh, mutate: func(input *RuleInput) {
			input.CornerAnalysis.FirstThrottleReapplication.TimeOffsetMs = 100
			input.Reference = &ReferenceMetrics{ExitSpeedKph: ScalarReference{Available: true, Value: 161}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := Generate(ruleInput(tt.mutate))
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			event := requireEvent(t, events, tt.eventType)
			if event.Severity != tt.want {
				t.Fatalf("expected severity %s, got %+v", tt.want, event)
			}
		})
	}
}

func TestGenerateDoesNotLeakRawTelemetryMetricNames(t *testing.T) {
	events, err := Generate(ruleInput(nil))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	payload, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("expected events to marshal: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{`"positionX"`, `"speedMps"`, `"throttle"`, `"brake"`, `"steering"`, `"wheelSpeedFL"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("expected generated events not to leak raw telemetry metric %s: %s", forbidden, body)
		}
	}
}

func TestGenerateAllowsNullableCatalogRefs(t *testing.T) {
	events, err := Generate(ruleInput(func(input *RuleInput) {
		input.Track = nil
		input.Layout = nil
		input.Corner = nil
	}))
	if err != nil {
		t.Fatalf("expected nullable refs to validate, got %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected generated events with nullable refs, got %+v", events)
	}
	for _, event := range events {
		if event.Track != nil || event.Layout != nil || event.Corner != nil {
			t.Fatalf("expected nil catalog refs, got %+v", event)
		}
	}
}

func ruleInput(mutate func(*RuleInput)) RuleInput {
	input := RuleInput{
		SessionID:       "session-1",
		LapNumber:       3,
		TimestampUnixMs: 1520,
		CornerAnalysis: corners.Analysis{
			Status:   corners.AnalysisStatusComplete,
			CornerID: "corner-1",
			ExitSpeedKph: corners.ScalarMetric{
				Status: corners.MetricStatusAvailable,
				Value:  144,
			},
			FirstThrottleReapplication: corners.PointMetric{
				Status:            corners.MetricStatusAvailable,
				DistanceFromStart: 162,
				TimestampUnixMs:   1520,
				TimeOffsetMs:      520,
			},
		},
		Reference: &ReferenceMetrics{ExitSpeedKph: ScalarReference{Available: true, Value: 158}},
		Track:     &CatalogRef{ID: testStringPtr("track-1"), Name: testStringPtr("Catalog Track"), DisplayStrategy: DisplayStrategyCatalogName},
		Layout:    &CatalogRef{ID: testStringPtr("layout-1"), Name: testStringPtr("Catalog Layout"), DisplayStrategy: DisplayStrategyCatalogName},
		Corner:    &CatalogRef{ID: testStringPtr("corner-1"), Name: testStringPtr("Catalog Corner"), DisplayStrategy: DisplayStrategyCatalogName},
	}
	if mutate != nil {
		mutate(&input)
	}
	return input
}

func requireEvent(t *testing.T, events []EngineerEvent, eventType EventType) EngineerEvent {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType {
			return event
		}
	}
	t.Fatalf("expected event type %s in %+v", eventType, events)
	return EngineerEvent{}
}

func assertMetric(t *testing.T, event EngineerEvent, name string, value float64, unit string, role MetricRole) {
	t.Helper()
	for _, metric := range event.Metrics {
		if metric.Name == name {
			if metric.Value != value || metric.Unit != unit || metric.Status != MetricStatusAvailable || metric.Role != role {
				t.Fatalf("expected metric %s=%v %s/%s, got %+v", name, value, unit, role, metric)
			}
			return
		}
	}
	t.Fatalf("expected metric %s in %+v", name, event.Metrics)
}

func testStringPtr(value string) *string {
	return &value
}
