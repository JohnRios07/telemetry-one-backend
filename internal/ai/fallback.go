package ai

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"telemetry-one-backend/internal/events"
)

const (
	StatusSuccess          = "success"
	StatusDegradedFallback = "degraded_fallback"
	StatusProviderError    = "provider_error"
	StatusInvalidResponse  = "invalid_response"
	StatusBudgetLimited    = "budget_limited"
	StatusRateLimited      = "rate_limited"
)

var ErrInvalidProviderResponse = errors.New("AI provider returned empty or malformed response")

type FallbackService struct {
	inner          AIService
	contextBuilder ContextBuilder
}

func NewFallbackService(inner AIService) *FallbackService {
	return &FallbackService{
		inner:          inner,
		contextBuilder: NewContextBuilder(),
	}
}

func (s *FallbackService) Analyze(ctx context.Context, req GatewayRequest) (GatewayResponse, error) {
	if err := req.Validate(); err != nil {
		return GatewayResponse{}, err
	}

	resp, err := s.inner.Analyze(ctx, req)
	if err == nil {
		if resp.Summary == "" || len(resp.ReferencedEvents) == 0 {
			return s.buildFallbackResponse(req, ErrInvalidProviderResponse, StatusInvalidResponse), nil
		}
		resp.Status = StatusSuccess
		return resp, nil
	}

	status := statusFromError(err)
	return s.buildFallbackResponse(req, err, status), nil
}

func (s *FallbackService) buildFallbackResponse(req GatewayRequest, err error, status string) GatewayResponse {
	summary := s.buildFallbackSummary(req.Input, status)
	referencedEvents := extractEventIDs(req.Input.Events)

	return GatewayResponse{
		Summary:          summary,
		EventExplanations: nil,
		Recommendations:  nil,
		ReferencedEvents: referencedEvents,
		ProviderInfo: ProviderResultInfo{
			FinishReason: status,
			Usage:        UsageInfo{},
		},
		Status: status,
		Error:  err.Error(),
	}
}

func (s *FallbackService) buildFallbackSummary(input ConsumerInput, status string) string {
	contextSummary := s.contextBuilder.Build(input)

	var parts []string
	parts = append(parts, fmt.Sprintf("AI analysis is currently unavailable (%s).", status))
	parts = append(parts, "")
	parts = append(parts, "The following summary is based on deterministic event data:")
	parts = append(parts, "")

	trackInfo := "unknown"
	if contextSummary.Session.Track != "" {
		trackInfo = contextSummary.Session.Track
	}
	layoutInfo := "unknown"
	if contextSummary.Session.Layout != "" {
		layoutInfo = contextSummary.Session.Layout
	}

	parts = append(parts, fmt.Sprintf("Session: %s", contextSummary.Session.SessionID))
	parts = append(parts, fmt.Sprintf("Track: %s", trackInfo))
	parts = append(parts, fmt.Sprintf("Layout: %s", layoutInfo))
	parts = append(parts, "")
	parts = append(parts, fmt.Sprintf("Engineer Events (%d total):", len(contextSummary.Events)))

	sortedEvents := sortBySeverityDesc(contextSummary.Events)
	displayCount := min(3, len(sortedEvents))
	for _, ev := range sortedEvents[:displayCount] {
		cornerStr := "unknown corner"
		if ev.Corner != "" {
			cornerStr = ev.Corner
		}
		parts = append(parts, fmt.Sprintf(
			"  [%s] %s - severity: %s, lap: %d, corner: %s",
			ev.EventID, ev.Type, ev.Severity, ev.LapNumber, cornerStr,
		))
		if ev.MetricSummary != "" {
			parts = append(parts, fmt.Sprintf("    metrics: %s", ev.MetricSummary))
		}
	}

	remaining := len(contextSummary.Events) - displayCount
	if remaining > 0 {
		parts = append(parts, fmt.Sprintf("  ... and %d more event(s)", remaining))
	}

	parts = append(parts, "")
	parts = append(parts, "No AI explanations or recommendations are available for this request.")
	parts = append(parts, "Review the highest severity events above for manual analysis.")

	return strings.Join(parts, "\n")
}

type severityWeight int

const (
	severityLow    severityWeight = 1
	severityMedium severityWeight = 2
	severityHigh   severityWeight = 3
)

func severityToWeight(s events.Severity) severityWeight {
	switch s {
	case events.SeverityLow:
		return severityLow
	case events.SeverityMedium:
		return severityMedium
	case events.SeverityHigh:
		return severityHigh
	default:
		return 0
	}
}

func sortBySeverityDesc(events []EventSummary) []EventSummary {
	sorted := make([]EventSummary, len(events))
	copy(sorted, events)
	sort.SliceStable(sorted, func(i, j int) bool {
		return severityToWeight(sorted[i].Severity) > severityToWeight(sorted[j].Severity)
	})
	return sorted
}

func statusFromError(err error) string {
	if errors.Is(err, ErrBudgetExceeded) {
		return StatusBudgetLimited
	}
	if errors.Is(err, ErrRateLimited) {
		return StatusRateLimited
	}
	if errors.Is(err, ErrRetryExhausted) ||
		errors.Is(err, ErrProviderNotAvailable) ||
		errors.Is(err, ErrProviderTimeout) {
		return StatusProviderError
	}
	if errors.Is(err, ErrInvalidProviderResponse) {
		return StatusInvalidResponse
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return StatusProviderError
	}
	return StatusProviderError
}

var _ AIService = (*FallbackService)(nil)
