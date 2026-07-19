package ai

import (
	"context"
	"errors"
	"testing"

	"telemetry-one-backend/internal/events"
)

type serviceTestAdapter struct {
	result ProviderResponse
	err    error
}

func (a *serviceTestAdapter) Analyze(_ context.Context, _ ProviderRequest) (ProviderResponse, error) {
	return a.result, a.err
}

func TestServiceSatisfiesAIServiceInterface(t *testing.T) {
	adapter := &serviceTestAdapter{
		result: ProviderResponse{
			Content:      "AI analysis result for the session",
			Model:        "test-model",
			FinishReason: "stop",
			Usage:        UsageInfo{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
		},
	}
	controller := &Controller{Budget: DefaultTokenBudget(), Retry: DefaultRetryPolicy()}
	svc := NewService(adapter, controller)

	req := validGatewayRequest(t, GatewayModeEngineer)
	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if resp.Summary != "AI analysis result for the session" {
		t.Fatalf("expected summary from adapter, got %q", resp.Summary)
	}
	if len(resp.ReferencedEvents) != 1 {
		t.Fatalf("expected 1 referenced event, got %d", len(resp.ReferencedEvents))
	}
}

func TestServiceAcceptsSignalsOnlyWithoutGeometry(t *testing.T) {
	adapter := &serviceTestAdapter{
		result: ProviderResponse{
			Content:      "Signals-only advice: focus on lap consistency and telemetry reliability.",
			Model:        "test-model",
			FinishReason: "stop",
			Usage:        UsageInfo{PromptTokens: 8, CompletionTokens: 12, TotalTokens: 20},
		},
	}
	controller := &Controller{Budget: DefaultTokenBudget(), Retry: DefaultRetryPolicy()}
	svc := NewService(adapter, controller)

	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.Events = nil
	req.Input.Session.Track = nil
	req.Input.Session.Layout = nil
	req.Input.Signals = []Signal{{Kind: "telemetry_gap_warning", Severity: events.SeverityLow, Summary: "Telemetry gaps were recorded on 2 completed laps."}}

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected signals-only request without geometry to succeed: %v", err)
	}
	if resp.Summary == "" {
		t.Fatal("expected non-empty advice summary")
	}
	if len(resp.Signals) != 1 || resp.Signals[0].Kind != "telemetry_gap_warning" {
		t.Fatalf("expected signals to round-trip through production service, got %+v", resp.Signals)
	}
}

func TestServiceRejectsInvalidRequest(t *testing.T) {
	adapter := &serviceTestAdapter{
		result: ProviderResponse{Content: "should not be called"},
	}
	controller := &Controller{Budget: DefaultTokenBudget(), Retry: DefaultRetryPolicy()}
	svc := NewService(adapter, controller)

	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.ContractVersion = ""

	_, err := svc.Analyze(context.Background(), req)
	if !errors.Is(err, ErrEmptyGatewayRequest) {
		t.Fatalf("expected ErrEmptyGatewayRequest, got %v", err)
	}
}

func TestServiceReturnsBudgetExceeded(t *testing.T) {
	adapter := &serviceTestAdapter{
		result: ProviderResponse{Content: "should not be called"},
	}
	budget := DefaultTokenBudget()
	budget.MaxPromptChars = 5
	controller := &Controller{Budget: budget, Retry: DefaultRetryPolicy()}
	svc := NewService(adapter, controller)

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got %v", err)
	}
}

func TestServiceRetryableErrorReturnsRetryExhausted(t *testing.T) {
	adapter := &serviceTestAdapter{
		err: ErrProviderNotAvailable,
	}
	retry := DefaultRetryPolicy()
	retry.MaxAttempts = 1
	controller := &Controller{Budget: DefaultTokenBudget(), Retry: retry}
	svc := NewService(adapter, controller)

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if !errors.Is(err, ErrRetryExhausted) {
		t.Fatalf("expected ErrRetryExhausted, got %v", err)
	}
}

func TestServiceNonRetryableErrorPropagates(t *testing.T) {
	adapter := &serviceTestAdapter{
		err: ErrProviderRejected,
	}
	controller := &Controller{Budget: DefaultTokenBudget(), Retry: DefaultRetryPolicy()}
	svc := NewService(adapter, controller)

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if !errors.Is(err, ErrProviderRejected) {
		t.Fatalf("expected ErrProviderRejected, got %v", err)
	}
}

func TestServiceWithModelOverride(t *testing.T) {
	adapter := &serviceTestAdapter{
		result: ProviderResponse{
			Content:      "test",
			Model:        "custom-model-v2",
			FinishReason: "stop",
		},
	}
	controller := &Controller{Budget: DefaultTokenBudget(), Retry: DefaultRetryPolicy()}
	svc := NewService(adapter, controller).WithModel("custom-model-v2")

	req := validGatewayRequest(t, GatewayModeEngineer)
	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if resp.ProviderInfo.Model != "custom-model-v2" {
		t.Fatalf("expected custom model, got %q", resp.ProviderInfo.Model)
	}
}

func TestServiceCoachModeSucceeds(t *testing.T) {
	adapter := &serviceTestAdapter{
		result: ProviderResponse{
			Content:      "coaching advice",
			Model:        "test-model",
			FinishReason: "stop",
		},
	}
	controller := &Controller{Budget: DefaultTokenBudget(), Retry: DefaultRetryPolicy()}
	svc := NewService(adapter, controller)

	req := validGatewayRequest(t, GatewayModeCoach)
	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected coach mode to succeed: %v", err)
	}
	if resp.Summary != "coaching advice" {
		t.Fatalf("expected coaching advice, got %q", resp.Summary)
	}
}
