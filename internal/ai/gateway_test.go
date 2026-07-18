package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"telemetry-one-backend/internal/events"
)

func TestGatewayRequestValidateAcceptsValidEngineerMode(t *testing.T) {
	req := validGatewayRequest(t, GatewayModeEngineer)

	if err := req.Validate(); err != nil {
		t.Fatalf("expected valid engineer gateway request: %v", err)
	}
}

func TestGatewayRequestValidateAcceptsValidCoachMode(t *testing.T) {
	req := validGatewayRequest(t, GatewayModeCoach)

	if err := req.Validate(); err != nil {
		t.Fatalf("expected valid coach gateway request: %v", err)
	}
}

func TestGatewayRequestValidateRejectsEmptyContract(t *testing.T) {
	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.ContractVersion = ""

	err := req.Validate()
	if !errors.Is(err, ErrEmptyGatewayRequest) {
		t.Fatalf("expected empty gateway request, got %v", err)
	}
}

func TestGatewayRequestValidateRejectsUnsupportedMode(t *testing.T) {
	req := validGatewayRequest(t, "unknown_mode")

	err := req.Validate()
	if !errors.Is(err, ErrUnsupportedGatewayMode) {
		t.Fatalf("expected unsupported mode error, got %v", err)
	}
}

func TestGatewayRequestValidateRejectsNoEvents(t *testing.T) {
	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.Events = nil

	err := req.Validate()
	if !errors.Is(err, ErrNoEventsInGateway) {
		t.Fatalf("expected no events error, got %v", err)
	}
}

func TestGatewayRequestValidateAcceptsSignalsOnly(t *testing.T) {
	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.Events = nil
	req.Input.Signals = []Signal{{Kind: "telemetry_gap_warning", Severity: events.SeverityLow, Summary: "Telemetry gaps were recorded on 2 completed laps."}}

	if err := req.Validate(); err != nil {
		t.Fatalf("expected gateway request with derived signals to validate, got %v", err)
	}
}

func TestGatewayRequestRejectsRawTelemetryViaConsumerInput(t *testing.T) {
	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.Events[0].Event.Metrics = []events.MetricEvidence{
		{Name: "speedMps", Value: 58.33, Unit: "m/s", Status: events.MetricStatusAvailable, Role: events.MetricRoleActual},
	}

	err := req.Validate()
	if !errors.Is(err, ErrInvalidEvent) || !strings.Contains(err.Error(), events.ErrRawTelemetryMetric.Error()) {
		t.Fatalf("expected invalid event with raw telemetry metric rejection in gateway, got %v", err)
	}
}

func TestGatewayRequestRejectsJSONRawTelemetryFields(t *testing.T) {
	payload := strings.Replace(validConsumerInputJSON(t), `"safety":`, `"frames":[{"positionX":123.4}],"safety":`, 1)

	input, err := DecodeConsumerInputJSON([]byte(payload))
	if !errors.Is(err, ErrRawTelemetryField) {
		t.Fatalf("expected raw telemetry field rejection at decode level, got %v / input=%+v", err, input)
	}
}

func TestGatewayRequestJSONDoesNotLeakRawTelemetry(t *testing.T) {
	req := validGatewayRequest(t, GatewayModeEngineer)
	payload := marshalGatewayRequest(t, req)

	for _, forbidden := range []string{`"positionX":`, `"positionY":`, `"speedMps":`, `"throttle":`, `"brake":`, `"frames":`} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("expected gateway request JSON not to contain raw telemetry field %q", forbidden)
		}
	}
}

func TestGatewayRequestAllowsExplicitUnknownCatalogRefs(t *testing.T) {
	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.Session.Track = nil
	req.Input.Session.Layout = nil
	req.Input.Events[0].Event.Track = nil
	req.Input.Events[0].Event.Layout = nil
	req.Input.Events[0].Event.Corner = nil
	req.Input.Constraints = append(req.Input.Constraints, "track, layout, and corner may be null when unknown; do not invent names")

	if err := req.Validate(); err != nil {
		t.Fatalf("expected unknown catalog refs to remain valid in gateway: %v", err)
	}
}

func TestGatewayResponseEventExplanationsAreTyped(t *testing.T) {
	resp := GatewayResponse{
		Summary: "test summary",
		EventExplanations: []EventExplanation{
			{EventID: "event-1", Type: events.TypeLateThrottle, Explanation: "late throttle on exit", Relevance: 0.85},
		},
		Recommendations: []string{"work on earlier throttle application"},
		ReferencedEvents: []string{"event-1"},
	}

	if len(resp.EventExplanations) != 1 {
		t.Fatalf("expected 1 explanation, got %d", len(resp.EventExplanations))
	}
	if resp.EventExplanations[0].EventID != "event-1" {
		t.Fatalf("expected event-1, got %s", resp.EventExplanations[0].EventID)
	}
}

func TestProviderAdapterInterfaceIsSatisfied(t *testing.T) {
	var adapter ProviderAdapter = &fakeProvider{}

	resp, err := adapter.Analyze(context.Background(), ProviderRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: "user", Content: "test"},
		},
		Temperature: 0.7,
		MaxTokens:   500,
	})
	if err != nil {
		t.Fatalf("expected fake provider to succeed: %v", err)
	}
	if resp.Content == "" {
		t.Fatalf("expected non-empty response from fake provider")
	}
}

func TestAIServiceInterfaceIsSatisfied(t *testing.T) {
	var svc AIService = &fakeAIService{}

	req := validGatewayRequest(t, GatewayModeEngineer)
	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fake service to succeed: %v", err)
	}
	if resp.Summary == "" {
		t.Fatalf("expected non-empty summary from fake service")
	}
}

type fakeProvider struct{}

func (f *fakeProvider) Analyze(_ context.Context, req ProviderRequest) (ProviderResponse, error) {
	return ProviderResponse{
		Content:      "AI analysis result for " + req.Messages[0].Content,
		Model:        req.Model,
		FinishReason: "stop",
		Usage:        UsageInfo{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}, nil
}

type fakeAIService struct{}

func (f *fakeAIService) Analyze(_ context.Context, req GatewayRequest) (GatewayResponse, error) {
	explanations := make([]EventExplanation, len(req.Input.Events))
	for i, env := range req.Input.Events {
		explanations[i] = EventExplanation{
			EventID:     env.Event.EventID,
			Type:        env.Event.Type,
			Explanation: "AI analysis for event " + env.Event.EventID,
			Relevance:   0.75,
		}
	}
	return GatewayResponse{
		Summary:           "Session " + req.Input.Session.SessionID + " analyzed",
		EventExplanations: explanations,
		Recommendations:   []string{"work on consistency"},
		ReferencedEvents:  extractEventIDs(req.Input.Events),
		ProviderInfo: ProviderResultInfo{
			Model:        "test-model",
			FinishReason: "stop",
			Usage:        UsageInfo{PromptTokens: 50, CompletionTokens: 100, TotalTokens: 150},
		},
	}, nil
}

func validGatewayRequest(t *testing.T, mode string) GatewayRequest {
	t.Helper()
	return GatewayRequest{
		Input: validConsumerInput(),
		Mode:  mode,
	}
}

func marshalGatewayRequest(t *testing.T, req GatewayRequest) []byte {
	t.Helper()

	payload, err := json.Marshal(req.Input)
	if err != nil {
		t.Fatalf("expected gateway request input to marshal: %v", err)
	}

	return payload
}

