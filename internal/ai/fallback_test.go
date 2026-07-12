package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"telemetry-one-backend/internal/events"
)

type fallbackTestAIService struct {
	result GatewayResponse
	err    error
}

func (f *fallbackTestAIService) Analyze(_ context.Context, _ GatewayRequest) (GatewayResponse, error) {
	return f.result, f.err
}

func TestFallbackServiceSatisfiesAIService(t *testing.T) {
	var svc AIService = NewFallbackService(&fakeAIService{})
	req := validGatewayRequest(t, GatewayModeEngineer)
	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success pass-through: %v", err)
	}
	if resp.Status != StatusSuccess {
		t.Fatalf("expected success status, got %s", resp.Status)
	}
	if resp.Summary == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestFallbackServicePassesThroughSuccessfulResponse(t *testing.T) {
	svc := NewFallbackService(&fakeAIService{})
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error: %v", err)
	}
	if resp.Status != StatusSuccess {
		t.Fatalf("expected success status, got %s", resp.Status)
	}
	if !strings.Contains(resp.Summary, "session-1") {
		t.Fatalf("expected original summary, got %s", resp.Summary)
	}
	if len(resp.EventExplanations) != 1 {
		t.Fatalf("expected 1 event explanation, got %d", len(resp.EventExplanations))
	}
	if len(resp.Recommendations) == 0 {
		t.Fatal("expected recommendations from pass-through")
	}
}

func TestFallbackServiceProviderErrorFallback(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if resp.Status != StatusProviderError {
		t.Fatalf("expected provider_error status, got %s", resp.Status)
	}
	if resp.Summary == "" {
		t.Fatal("expected non-empty fallback summary")
	}
	if !strings.Contains(resp.Summary, "AI analysis is currently unavailable") {
		t.Fatalf("expected fallback summary to indicate unavailability, got %s", resp.Summary)
	}
	if strings.Contains(resp.Summary, "ErrProviderNotAvailable") {
		t.Fatalf("fallback summary should not contain raw error text, got %s", resp.Summary)
	}
}

func TestFallbackServiceTimeoutFallback(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderTimeout}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if resp.Status != StatusProviderError {
		t.Fatalf("expected provider_error status, got %s", resp.Status)
	}
}

type contextAwareTestService struct {
	err error
}

func (s *contextAwareTestService) Analyze(ctx context.Context, _ GatewayRequest) (GatewayResponse, error) {
	if ctx.Err() != nil {
		return GatewayResponse{}, ctx.Err()
	}
	if s.err != nil {
		return GatewayResponse{}, s.err
	}
	return GatewayResponse{
		Summary:          "session analysis",
		ReferencedEvents: []string{"event-1"},
		ProviderInfo:     ProviderResultInfo{Model: "test-model", FinishReason: "stop", Usage: UsageInfo{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
	}, nil
}

func TestFallbackServiceContextCanceledFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := NewFallbackService(&contextAwareTestService{})
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(ctx, req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if resp.Status != StatusProviderError {
		t.Fatalf("expected provider_error status, got %s", resp.Status)
	}
}

func TestFallbackServiceInvalidResponseEmptySummary(t *testing.T) {
	inner := &fallbackTestAIService{
		result: GatewayResponse{
			Summary:           "",
			EventExplanations: []EventExplanation{{EventID: "event-1"}},
			ReferencedEvents:  []string{"event-1"},
			ProviderInfo:      ProviderResultInfo{Model: "test-model", FinishReason: "stop", Usage: UsageInfo{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
		},
	}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if resp.Status != StatusInvalidResponse {
		t.Fatalf("expected invalid_response status, got %s", resp.Status)
	}
}

func TestFallbackServiceInvalidResponseNoReferencedEvents(t *testing.T) {
	inner := &fallbackTestAIService{
		result: GatewayResponse{
			Summary:           "some summary",
			EventExplanations: []EventExplanation{{EventID: "event-1"}},
			ReferencedEvents:  nil,
			ProviderInfo:      ProviderResultInfo{Model: "test-model", FinishReason: "stop", Usage: UsageInfo{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
		},
	}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if resp.Status != StatusInvalidResponse {
		t.Fatalf("expected invalid_response status, got %s", resp.Status)
	}
}

func TestFallbackServiceProviderErrorAfterRetryExhausted(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrRetryExhausted}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if resp.Status != StatusProviderError {
		t.Fatalf("expected provider_error status, got %s", resp.Status)
	}
}

func TestFallbackServiceBudgetLimitedFallback(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrBudgetExceeded}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if resp.Status != StatusBudgetLimited {
		t.Fatalf("expected budget_limited status, got %s", resp.Status)
	}
}

func TestFallbackServiceRateLimitedFallback(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrRateLimited}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if resp.Status != StatusRateLimited {
		t.Fatalf("expected rate_limited status, got %s", resp.Status)
	}
}

func TestFallbackServiceContainsSourceEventIDs(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}

	if len(resp.ReferencedEvents) != 1 || resp.ReferencedEvents[0] != "event-1" {
		t.Fatalf("expected referenced events [event-1], got %v", resp.ReferencedEvents)
	}
	if !strings.Contains(resp.Summary, "event-1") {
		t.Fatalf("expected fallback summary to contain event ID event-1, got %s", resp.Summary)
	}
}

func TestFallbackServiceSummaryContainsEventTypesAndSeverities(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}

	if !strings.Contains(resp.Summary, string(events.TypeLateThrottle)) {
		t.Fatalf("expected fallback summary to contain event type, got %s", resp.Summary)
	}
	if !strings.Contains(resp.Summary, string(events.SeverityMedium)) {
		t.Fatalf("expected fallback summary to contain severity, got %s", resp.Summary)
	}
}

func TestFallbackSummaryNoRawTelemetryLeakage(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}

	for _, field := range []string{"positionX", "positionY", "speedMps", "rpm", "steering", "wheelSpeedFL", "fuelLiters", "frames", "telemetryFrames"} {
		if strings.Contains(resp.Summary, field) {
			t.Fatalf("fallback summary must not contain raw telemetry field %q", field)
		}
	}
}

func TestFallbackSummaryNoAIExplanationText(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}

	if strings.Contains(resp.Summary, "AI analysis") && !strings.Contains(resp.Summary, "unavailable") {
		t.Fatalf("fallback summary must not pretend AI analysis is available, got %s", resp.Summary)
	}
}

func TestFallbackServiceFallbackNoRecommendations(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if len(resp.Recommendations) != 0 {
		t.Fatalf("expected empty recommendations on fallback, got %v", resp.Recommendations)
	}
	if len(resp.EventExplanations) != 0 {
		t.Fatalf("expected empty event explanations on fallback, got %v", resp.EventExplanations)
	}
}

func TestFallbackServiceErrorFieldContainsOriginalError(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if resp.Error == "" {
		t.Fatal("expected non-empty Error field on fallback")
	}
	if !strings.Contains(resp.Error, "not available") {
		t.Fatalf("expected Error to mention 'not available', got %s", resp.Error)
	}
}

func TestFallbackServiceDeterministicSameErrorSameFallback(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp1, _ := svc.Analyze(context.Background(), req)
	resp2, _ := svc.Analyze(context.Background(), req)

	if resp1.Summary != resp2.Summary {
		t.Fatal("expected fallback summaries to be identical for same input and error")
	}
}

func TestFallbackServiceInvalidRequestNotFallback(t *testing.T) {
	svc := NewFallbackService(&fakeAIService{})
	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.ContractVersion = ""

	_, err := svc.Analyze(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for invalid request, not fallback")
	}
}

func TestFallbackServiceSortsEventsBySeverity(t *testing.T) {
	input := validConsumerInput()

	second := validConsumerInput().Events[0]
	second.Event.EventID = "event-high"
	second.Event.Type = events.TypeLowExitSpeed
	second.Event.Severity = events.SeverityHigh
	input.Events = append(input.Events, second)

	third := validConsumerInput().Events[0]
	third.Event.EventID = "event-low"
	third.Event.Type = events.TypeEarlyBraking
	third.Event.Severity = events.SeverityLow
	input.Events = append(input.Events, third)

	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := GatewayRequest{Input: input, Mode: GatewayModeEngineer}

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response: %v", err)
	}

	highIdx := strings.Index(resp.Summary, "event-high")
	mediumIdx := strings.Index(resp.Summary, "event-1")
	lowIdx := strings.Index(resp.Summary, "event-low")

	if highIdx < 0 || mediumIdx < 0 || lowIdx < 0 {
		t.Fatalf("expected all three event IDs in fallback summary")
	}
	if !(highIdx < mediumIdx && mediumIdx < lowIdx) {
		t.Fatalf("expected events sorted by severity: high before medium before low")
	}
}

func TestFallbackServiceLimitsDisplayedEvents(t *testing.T) {
	input := validConsumerInput()
	for i := range 10 {
		ev := validConsumerInput().Events[0]
		ev.Event.EventID = fmt.Sprintf("event-%d", i+2)
		input.Events = append(input.Events, ev)
	}

	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := GatewayRequest{Input: input, Mode: GatewayModeEngineer}

	resp, _ := svc.Analyze(context.Background(), req)

	if !strings.Contains(resp.Summary, "and 8 more event(s)") {
		t.Fatalf("expected 'and 8 more event(s)' in summary for 11 total events, got: %s", resp.Summary)
	}
}

func TestFallbackServiceWithUnknownCatalogRefs(t *testing.T) {
	input := validConsumerInput()
	input.Session.Track = nil
	input.Session.Layout = nil
	input.Events[0].Event.Corner = nil

	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := GatewayRequest{Input: input, Mode: GatewayModeEngineer}

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response: %v", err)
	}

	if strings.Contains(resp.Summary, "Watkins Glen") {
		t.Fatalf("fallback summary should not contain invented track name when catalog ref is nil")
	}
	if !strings.Contains(strings.ToLower(resp.Summary), "unknown") {
		t.Fatalf("fallback summary should indicate unknown for nil refs, got: %s", resp.Summary)
	}
}

func TestFallbackServiceErrorNoModelOrUsageInProviderInfo(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response: %v", err)
	}
	if resp.ProviderInfo.Model != "" {
		t.Fatalf("expected empty model on fallback, got %s", resp.ProviderInfo.Model)
	}
	if resp.ProviderInfo.Usage.TotalTokens != 0 {
		t.Fatalf("expected zero usage on fallback, got %d", resp.ProviderInfo.Usage.TotalTokens)
	}
}

func TestStatusFromErrorBudgetExceeded(t *testing.T) {
	if s := statusFromError(ErrBudgetExceeded); s != StatusBudgetLimited {
		t.Fatalf("expected budget_limited, got %s", s)
	}
}

func TestStatusFromErrorRateLimited(t *testing.T) {
	if s := statusFromError(ErrRateLimited); s != StatusRateLimited {
		t.Fatalf("expected rate_limited, got %s", s)
	}
}

func TestStatusFromErrorProviderErrors(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrProviderNotAvailable, StatusProviderError},
		{ErrProviderTimeout, StatusProviderError},
		{ErrRetryExhausted, StatusProviderError},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			if s := statusFromError(tt.err); s != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, s)
			}
		})
	}
}

func TestStatusFromErrorInvalidResponse(t *testing.T) {
	if s := statusFromError(ErrInvalidProviderResponse); s != StatusInvalidResponse {
		t.Fatalf("expected invalid_response, got %s", s)
	}
}

func TestStatusFromErrorContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s := statusFromError(ctx.Err()); s != StatusProviderError {
		t.Fatalf("expected provider_error for canceled ctx, got %s", s)
	}
}

func TestStatusFromErrorUnknownErrorDefaultsProviderError(t *testing.T) {
	if s := statusFromError(errors.New("some unknown error")); s != StatusProviderError {
		t.Fatalf("expected provider_error for unknown error, got %s", s)
	}
}

func TestSortBySeverityDesc(t *testing.T) {
	events := []EventSummary{
		{EventID: "low", Severity: events.SeverityLow},
		{EventID: "high", Severity: events.SeverityHigh},
		{EventID: "medium", Severity: events.SeverityMedium},
	}

	sorted := sortBySeverityDesc(events)
	if sorted[0].EventID != "high" || sorted[1].EventID != "medium" || sorted[2].EventID != "low" {
		t.Fatalf("expected high > medium > low, got %v", sorted)
	}
}

func TestSortBySeverityDescEmpty(t *testing.T) {
	sorted := sortBySeverityDesc(nil)
	if len(sorted) != 0 {
		t.Fatalf("expected empty result for nil input, got %d", len(sorted))
	}
	sorted = sortBySeverityDesc([]EventSummary{})
	if len(sorted) != 0 {
		t.Fatalf("expected empty result for empty input, got %d", len(sorted))
	}
}

func TestSeverityToWeight(t *testing.T) {
	if w := severityToWeight(events.SeverityLow); w != severityLow {
		t.Fatalf("expected severityLow weight %d, got %d", severityLow, w)
	}
	if w := severityToWeight(events.SeverityMedium); w != severityMedium {
		t.Fatalf("expected severityMedium weight %d, got %d", severityMedium, w)
	}
	if w := severityToWeight(events.SeverityHigh); w != severityHigh {
		t.Fatalf("expected severityHigh weight %d, got %d", severityHigh, w)
	}
	if w := severityToWeight("unknown"); w != 0 {
		t.Fatalf("expected 0 for unknown severity, got %d", w)
	}
}

func TestFallbackServiceIsThreadSafe(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	errs := make(chan error, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := svc.Analyze(context.Background(), req)
			if err != nil {
				errs <- fmt.Errorf("expected fallback response: %w", err)
				return
			}
			if resp.Status != StatusProviderError {
				errs <- fmt.Errorf("expected provider_error, got %s", resp.Status)
			}
		}()
	}
	wg.Wait()
	close(errs)

	for e := range errs {
		t.Error(e)
	}
}

func TestFallbackServiceErrorMentionsStatusInFinishReason(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrBudgetExceeded}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response: %v", err)
	}
	if resp.ProviderInfo.FinishReason != StatusBudgetLimited {
		t.Fatalf("expected finish reason %s, got %s", StatusBudgetLimited, resp.ProviderInfo.FinishReason)
	}
}

func TestAuditedServiceWithFallbackRecordsProviderErrorStatus(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	fallback := NewFallbackService(inner)
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(fallback, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}

	if record.Status != StatusProviderError {
		t.Fatalf("expected audit status %s for fallback, got %s", StatusProviderError, record.Status)
	}
	if record.ErrorMessage == "" {
		t.Fatal("expected non-empty error message in audit for fallback")
	}
	if !strings.Contains(record.ErrorMessage, "not available") {
		t.Fatalf("expected error message to mention 'not available', got %s", record.ErrorMessage)
	}
}

func TestAuditedServiceWithFallbackRecordsBudgetLimited(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrBudgetExceeded}
	fallback := NewFallbackService(inner)
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(fallback, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	svc.Analyze(context.Background(), req)

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if record.Status != StatusBudgetLimited {
		t.Fatalf("expected audit status %s, got %s", StatusBudgetLimited, record.Status)
	}
}

func TestAuditedServiceWithFallbackRecordsRateLimited(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrRateLimited}
	fallback := NewFallbackService(inner)
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(fallback, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	svc.Analyze(context.Background(), req)

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if record.Status != StatusRateLimited {
		t.Fatalf("expected audit status %s, got %s", StatusRateLimited, record.Status)
	}
}

func TestAuditedServiceWithFallbackRecordsInvalidResponse(t *testing.T) {
	inner := &fallbackTestAIService{
		result: GatewayResponse{
			Summary:           "",
			EventExplanations: []EventExplanation{{EventID: "event-1"}},
			ReferencedEvents:  []string{"event-1"},
			ProviderInfo:      ProviderResultInfo{Model: "test-model", FinishReason: "stop", Usage: UsageInfo{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
		},
	}
	fallback := NewFallbackService(inner)
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(fallback, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	svc.Analyze(context.Background(), req)

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if record.Status != StatusInvalidResponse {
		t.Fatalf("expected audit status %s, got %s", StatusInvalidResponse, record.Status)
	}
}

func TestAuditedServiceWithFallbackPreservesSourceEventIDs(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	fallback := NewFallbackService(inner)
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(fallback, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	svc.Analyze(context.Background(), req)

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if len(record.SourceEventIDs) != 1 || record.SourceEventIDs[0] != "event-1" {
		t.Fatalf("expected source event IDs to be preserved on fallback, got %v", record.SourceEventIDs)
	}
}

func TestAuditedServiceWithFallbackResponseSummaryNotTruncated(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	fallback := NewFallbackService(inner)
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(fallback, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	svc.Analyze(context.Background(), req)

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}

	if record.ResponseSummary == "" {
		t.Fatal("expected non-empty response summary in audit record for fallback")
	}
	if !strings.Contains(record.ResponseSummary, "AI analysis") && !strings.Contains(record.ResponseSummary, "unavailable") {
		t.Fatalf("expected response summary to indicate AI unavailability, got %s", record.ResponseSummary)
	}
}

type failingFallbackTestAdapter struct{}

func (f *failingFallbackTestAdapter) Analyze(_ context.Context, _ ProviderRequest) (ProviderResponse, error) {
	if f == nil {
		return ProviderResponse{}, nil
	}
	return ProviderResponse{}, ErrProviderNotAvailable
}

func TestFallbackWithControllerEndToEnd(t *testing.T) {
	adapter := &controlledTestAdapter{failWith: ErrProviderNotAvailable}
	ctrl := Controller{
		Budget: DefaultTokenBudget(),
		Retry: RetryPolicy{
			MaxAttempts:   2,
			BaseBackoffMs: 5,
			MaxBackoffMs:  10,
		},
	}

	controllerService := &controllerBackedService{
		controller:    ctrl,
		adapter:       adapter,
		promptBuilder: NewPromptBuilder(),
	}
	fallback := NewFallbackService(controllerService)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := fallback.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if resp.Status != StatusProviderError {
		t.Fatalf("expected provider_error status, got %s", resp.Status)
	}
	if !strings.Contains(resp.Summary, "AI analysis is currently unavailable") {
		t.Fatalf("expected fallback summary, got %s", resp.Summary)
	}
}

type controllerBackedService struct {
	controller    Controller
	adapter       ProviderAdapter
	promptBuilder PromptBuilder
}

func (s *controllerBackedService) Analyze(ctx context.Context, req GatewayRequest) (GatewayResponse, error) {
	prompts := s.promptBuilder.Build(req)
	promptCharCount := len(prompts.SystemPrompt) + len(prompts.UserPrompt)
	eventCount := len(req.Input.Events)

	providerReq := ProviderRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: "system", Content: prompts.SystemPrompt},
			{Role: "user", Content: prompts.UserPrompt},
		},
		Temperature: 0.7,
		MaxTokens:   2000,
	}

	_, err := s.controller.Execute(ctx, req.Input.Session.SessionID, req.Mode, promptCharCount, eventCount, providerReq, s.adapter)
	if err != nil {
		return GatewayResponse{}, err
	}

	return GatewayResponse{
		Summary: "Session analysis",
		Status:  StatusSuccess,
	}, nil
}

func TestFallbackServiceDoesNotFallbackOnValidEmptyExplanations(t *testing.T) {
	emptyExplanations := &fallbackTestAIService{
		result: GatewayResponse{
			Summary:           "No issues found - clean lap",
			EventExplanations: nil,
			Recommendations:   []string{"keep up the good work"},
			ReferencedEvents:  []string{"event-1"},
			ProviderInfo:      ProviderResultInfo{Model: "test-model", FinishReason: "stop", Usage: UsageInfo{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
		},
	}
	svc := NewFallbackService(emptyExplanations)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if resp.Status != StatusSuccess {
		t.Fatalf("expected success for valid response with empty explanations, got %s", resp.Status)
	}
	if resp.Summary != "No issues found - clean lap" {
		t.Fatalf("expected original summary, got %s", resp.Summary)
	}
}

func TestFallbackServiceNonRetryableProviderErrorFallback(t *testing.T) {
	inner := &fallbackTestAIService{err: ErrProviderRejected}
	svc := NewFallbackService(inner)
	req := validGatewayRequest(t, GatewayModeEngineer)

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if resp.Status != StatusProviderError {
		t.Fatalf("expected provider_error status, got %s", resp.Status)
	}
}

func TestFallbackServiceWithZeroEventsReturnsValidationError(t *testing.T) {
	svc := NewFallbackService(&fakeAIService{})
	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.Events = nil

	_, err := svc.Analyze(context.Background(), req)
	if err == nil {
		t.Fatal("expected validation error for empty events")
	}
}

func TestFallbackSummaryRespectsUnknownTrackLayoutCorner(t *testing.T) {
	input := validConsumerInput()
	input.Session.Track = nil
	input.Session.Layout = nil

	inner := &fallbackTestAIService{err: ErrProviderNotAvailable}
	svc := NewFallbackService(inner)
	req := GatewayRequest{Input: input, Mode: GatewayModeEngineer}

	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response: %v", err)
	}

	if strings.Contains(resp.Summary, "Watkins Glen") {
		t.Fatal("fallback summary must not contain catalog names when refs are nil")
	}
}
