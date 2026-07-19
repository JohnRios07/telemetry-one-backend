package ai

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"telemetry-one-backend/internal/events"
)

func TestConsumerInputValidateAcceptsStructuredEngineerEvents(t *testing.T) {
	input := validConsumerInput()

	if err := input.Validate(); err != nil {
		t.Fatalf("expected valid AI consumer input: %v", err)
	}
}

func TestConsumerInputValidateAcceptsDerivedSignalsWithoutEvents(t *testing.T) {
	input := validConsumerInput()
	input.Events = nil
	input.Session.Track = nil
	input.Session.Layout = nil
	input.Signals = []Signal{{Kind: "lap_pace_regression", Severity: events.SeverityMedium, Summary: "Latest completed lap is slower than the session best."}}

	if err := input.Validate(); err != nil {
		t.Fatalf("expected valid AI consumer input with derived signals only: %v", err)
	}
}

func TestDecodeConsumerInputJSONRejectsRawTelemetryFields(t *testing.T) {
	payload := strings.Replace(validConsumerInputJSON(t), `"safety":`, `"frames":[{"positionX":123.4,"speedMps":58.33,"throttle":0.82}],"safety":`, 1)

	_, err := DecodeConsumerInputJSON([]byte(payload))
	if !errors.Is(err, ErrRawTelemetryField) {
		t.Fatalf("expected raw telemetry field rejection, got %v", err)
	}
}

func TestConsumerInputValidateRejectsRawTelemetryMetricEvidence(t *testing.T) {
	input := validConsumerInput()
	input.Events[0].Event.Metrics = []events.MetricEvidence{
		{Name: "speedMps", Value: 58.33, Unit: "m/s", Status: events.MetricStatusAvailable, Role: events.MetricRoleActual},
	}

	err := input.Validate()
	if !errors.Is(err, ErrInvalidEvent) || !strings.Contains(err.Error(), events.ErrRawTelemetryMetric.Error()) {
		t.Fatalf("expected invalid event with raw telemetry metric rejection, got %v", err)
	}
}

func TestConsumerInputJSONDoesNotLeakRawTelemetryFields(t *testing.T) {
	payload := validConsumerInputJSON(t)

	for _, forbidden := range []string{`"positionX":`, `"positionY":`, `"positionZ":`, `"speedMps":`, `"throttle":`, `"brake":`, `"steering":`, `"wheelSpeedFL":`, `"frames":`} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("expected AI input JSON not to contain raw telemetry field %q: %s", forbidden, payload)
		}
	}
}

func TestConsumerInputAllowsExplicitUnknownCatalogRefs(t *testing.T) {
	input := validConsumerInput()
	input.Session.Track = nil
	input.Session.Layout = nil
	input.Events[0].Event.Track = nil
	input.Events[0].Event.Layout = nil
	input.Events[0].Event.Corner = nil
	input.Constraints = append(input.Constraints, "track, layout, and corner may be null when unknown; do not invent names")

	if err := input.Validate(); err != nil {
		t.Fatalf("expected unknown catalog refs to remain valid and explicit: %v", err)
	}
}

func TestConsumerInputValidateRequiresAllowedInputKinds(t *testing.T) {
	input := validConsumerInput()
	input.Safety.AllowedInputKinds = append(input.Safety.AllowedInputKinds, "raw_frames")

	err := input.Validate()
	if !errors.Is(err, ErrUnsupportedInputKind) {
		t.Fatalf("expected unsupported input kind rejection, got %v", err)
	}
}

func validConsumerInputJSON(t *testing.T) string {
	t.Helper()

	payload, err := json.Marshal(validConsumerInput())
	if err != nil {
		t.Fatalf("expected valid consumer input to marshal: %v", err)
	}

	return string(payload)
}

func validConsumerInput() ConsumerInput {
	trackID := "gt7_watkins_glen_international"
	trackName := "Watkins Glen International"
	layoutID := "gt7_watkins_glen_long_course"
	layoutName := "Long Course"
	cornerID := "corner-1"
	cornerName := "Catalog Corner"

	return ConsumerInput{
		ContractVersion: ContractVersionV1,
		Session: SessionContext{
			SessionID: "session-1",
			Track:     &events.CatalogRef{ID: &trackID, Name: &trackName, DisplayStrategy: events.DisplayStrategyCatalogName},
			Layout:    &events.CatalogRef{ID: &layoutID, Name: &layoutName, DisplayStrategy: events.DisplayStrategyCatalogName},
		},
		Events: []EventEnvelope{
			{Event: events.EngineerEvent{
				EventID:         "event-1",
				SessionID:       "session-1",
				Version:         events.ContractVersionV1,
				Type:            events.TypeLateThrottle,
				Severity:        events.SeverityMedium,
				Confidence:      0.82,
				TimestampUnixMs: 1720656012345,
				TimeRange:       &events.TimeRange{StartUnixMs: 1720656012000, EndUnixMs: 1720656012345},
				LapNumber:       2,
				Track:           &events.CatalogRef{ID: &trackID, Name: &trackName, DisplayStrategy: events.DisplayStrategyCatalogName},
				Layout:          &events.CatalogRef{ID: &layoutID, Name: &layoutName, DisplayStrategy: events.DisplayStrategyCatalogName},
				Corner:          &events.CatalogRef{ID: &cornerID, Name: &cornerName, DisplayStrategy: events.DisplayStrategyCatalogName},
				Metrics: []events.MetricEvidence{
					{Name: "throttleReapplicationDeltaMs", Value: 320, Unit: "ms", Status: events.MetricStatusAvailable, Role: events.MetricRoleDelta},
				},
				Source: events.EventSource{Kind: events.SourceDeterministicRule, RuleID: "late_throttle.v1", RuleVersion: "v1"},
			}},
		},
		Safety: SafetyMetadata{
			RedactionPolicy: "no raw telemetry frames, coordinates, control streams, or frame batches",
			AllowedInputKinds: []string{
				AllowedInputEngineerEvents,
				AllowedInputDerivedMetrics,
				AllowedInputCatalogRefs,
				AllowedInputSessionContext,
				UnknownStateExplicit,
			},
		},
		Constraints: []string{
			"AI receives structured Engineer events and derived metric evidence only",
			"catalog display names may only come from Telemetry One catalog metadata",
			"unknown track, layout, or corner states must remain explicit",
		},
	}
}
