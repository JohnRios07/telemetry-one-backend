package events

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestEngineerEventValidate(t *testing.T) {
	tests := []struct {
		name    string
		event   EngineerEvent
		wantErr error
	}{
		{name: "valid", event: validEvent()},
		{name: "missing event", event: withEvent(func(event *EngineerEvent) { event.EventID = "" }), wantErr: ErrMissingEventID},
		{name: "missing session", event: withEvent(func(event *EngineerEvent) { event.SessionID = "" }), wantErr: ErrMissingSessionID},
		{name: "missing version", event: withEvent(func(event *EngineerEvent) { event.Version = "" }), wantErr: ErrMissingVersion},
		{name: "unsupported version", event: withEvent(func(event *EngineerEvent) { event.Version = "telemetry-one.engineer-event.v2" }), wantErr: ErrUnsupportedVersion},
		{name: "missing type", event: withEvent(func(event *EngineerEvent) { event.Type = "" }), wantErr: ErrMissingType},
		{name: "unsupported type", event: withEvent(func(event *EngineerEvent) { event.Type = "weak_corner" }), wantErr: ErrUnsupportedType},
		{name: "missing severity", event: withEvent(func(event *EngineerEvent) { event.Severity = "" }), wantErr: ErrMissingSeverity},
		{name: "unsupported severity", event: withEvent(func(event *EngineerEvent) { event.Severity = "critical" }), wantErr: ErrUnsupportedSeverity},
		{name: "bad confidence", event: withEvent(func(event *EngineerEvent) { event.Confidence = 1.1 }), wantErr: ErrInvalidConfidence},
		{name: "bad timestamp", event: withEvent(func(event *EngineerEvent) { event.TimestampUnixMs = 0 }), wantErr: ErrInvalidTimestamp},
		{name: "bad time range", event: withEvent(func(event *EngineerEvent) { event.TimeRange = &TimeRange{StartUnixMs: 2000, EndUnixMs: 1999} }), wantErr: ErrInvalidTimeRange},
		{name: "bad lap", event: withEvent(func(event *EngineerEvent) { event.LapNumber = -1 }), wantErr: ErrInvalidLapNumber},
		{name: "missing metrics", event: withEvent(func(event *EngineerEvent) { event.Metrics = nil }), wantErr: ErrMissingMetrics},
		{name: "raw telemetry metric", event: withEvent(func(event *EngineerEvent) {
			event.Metrics = []MetricEvidence{{Name: "positionX", Value: 123, Status: MetricStatusAvailable}}
		}), wantErr: ErrRawTelemetryMetric},
		{name: "bad metric status", event: withEvent(func(event *EngineerEvent) {
			event.Metrics = []MetricEvidence{{Name: "exitSpeedKph", Value: 210, Status: "estimated"}}
		}), wantErr: ErrUnsupportedMetricStatus},
		{name: "bad metric role", event: withEvent(func(event *EngineerEvent) {
			event.Metrics = []MetricEvidence{{Name: "exitSpeedKph", Value: 210, Status: MetricStatusAvailable, Role: "prediction"}}
		}), wantErr: ErrUnsupportedMetricRole},
		{name: "bad source", event: withEvent(func(event *EngineerEvent) { event.Source.Kind = "ai_gateway" }), wantErr: ErrUnsupportedSourceKind},
		{name: "invented catalog name", event: withEvent(func(event *EngineerEvent) {
			event.Corner = &CatalogRef{ID: nil, Name: stringPtr("Turn 1"), DisplayStrategy: DisplayStrategyCatalogName}
		}), wantErr: ErrInvalidCatalogRef},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestEngineerEventJSONShapeIsStructuredForAIConsumers(t *testing.T) {
	payload, err := json.Marshal(validEvent())
	if err != nil {
		t.Fatalf("expected event to marshal: %v", err)
	}

	jsonBody := string(payload)
	for _, want := range []string{
		`"eventId":"event-1"`,
		`"version":"telemetry-one.engineer-event.v1"`,
		`"type":"late_throttle"`,
		`"severity":"medium"`,
		`"confidence":0.82`,
		`"timestampUnixMs":1720656012345`,
		`"track":{"id":"track-1","name":"Catalog Track","displayStrategy":"catalog_name"}`,
		`"layout":{"id":"layout-1","name":"Catalog Layout","displayStrategy":"catalog_name"}`,
		`"corner":{"id":"corner-1","name":"Catalog Corner","displayStrategy":"catalog_name"}`,
		`"metrics":[{"name":"throttleReapplicationDeltaMs","value":320,"unit":"ms","status":"available","role":"delta"}]`,
		`"source":{"kind":"deterministic_rule","ruleId":"late_throttle.v1","ruleVersion":"v1"}`,
	} {
		if !strings.Contains(jsonBody, want) {
			t.Fatalf("expected JSON to contain %s, got %s", want, jsonBody)
		}
	}
}

func TestEngineerEventJSONShapeAllowsNullableCatalogRefs(t *testing.T) {
	event := validEvent()
	event.Track = nil
	event.Layout = nil
	event.Corner = nil

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("expected event to marshal: %v", err)
	}
	jsonBody := string(payload)
	for _, want := range []string{`"track":null`, `"layout":null`, `"corner":null`} {
		if !strings.Contains(jsonBody, want) {
			t.Fatalf("expected nullable catalog ref %s, got %s", want, jsonBody)
		}
	}
}

func TestEngineerEventDoesNotLeakRawTelemetryPayloadFields(t *testing.T) {
	payload, err := json.Marshal(validEvent())
	if err != nil {
		t.Fatalf("expected event to marshal: %v", err)
	}

	jsonBody := string(payload)
	for _, forbidden := range []string{`"positionX":`, `"positionY":`, `"positionZ":`, `"speedMps":`, `"throttle":`, `"brake":`, `"steering":`, `"wheelSpeedFL":`} {
		if strings.Contains(jsonBody, forbidden) {
			t.Fatalf("expected event JSON not to contain raw telemetry field %q: %s", forbidden, jsonBody)
		}
	}
}

func validEvent() EngineerEvent {
	return EngineerEvent{
		EventID:         "event-1",
		SessionID:       "session-1",
		Version:         ContractVersionV1,
		Type:            TypeLateThrottle,
		Severity:        SeverityMedium,
		Confidence:      0.82,
		TimestampUnixMs: 1720656012345,
		TimeRange:       &TimeRange{StartUnixMs: 1720656012000, EndUnixMs: 1720656012345},
		LapNumber:       2,
		Track:           &CatalogRef{ID: stringPtr("track-1"), Name: stringPtr("Catalog Track"), DisplayStrategy: DisplayStrategyCatalogName},
		Layout:          &CatalogRef{ID: stringPtr("layout-1"), Name: stringPtr("Catalog Layout"), DisplayStrategy: DisplayStrategyCatalogName},
		Corner:          &CatalogRef{ID: stringPtr("corner-1"), Name: stringPtr("Catalog Corner"), DisplayStrategy: DisplayStrategyCatalogName},
		Metrics: []MetricEvidence{
			{Name: "throttleReapplicationDeltaMs", Value: 320, Unit: "ms", Status: MetricStatusAvailable, Role: MetricRoleDelta},
		},
		Source: EventSource{Kind: SourceDeterministicRule, RuleID: "late_throttle.v1", RuleVersion: "v1"},
	}
}

func withEvent(change func(*EngineerEvent)) EngineerEvent {
	event := validEvent()
	change(&event)

	return event
}

func stringPtr(value string) *string {
	return &value
}
