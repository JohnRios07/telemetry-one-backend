package ai

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"telemetry-one-backend/internal/config"
)

func TestComposePipelineDefaultFakeProvider(t *testing.T) {
	pipeCfg := PipelineConfig{
		AIMaxPromptChars:         40000,
		AIMaxCompletionTokens:    2000,
		AIMaxEventsPerRequest:    50,
		AIRetryMaxAttempts:       3,
		AIRetryBaseBackoffMs:     1000,
		AIRetryMaxBackoffMs:      10000,
		AIRatePerSecond:          10,
		AIRateBurst:              5,
		AICostPerPromptToken:     0.00015,
		AICostPerCompletionToken: 0.00060,
		AIModel:                  "gpt-4o-mini",
		AIProvider:               "",
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := ComposePipeline(pipeCfg, logger)

	var ais AIService = svc
	_ = ais

	req := validGatewayRequest(t, GatewayModeEngineer)
	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success with default fake provider: %v", err)
	}
	if resp.Status != StatusSuccess {
		t.Fatalf("expected success status, got %s", resp.Status)
	}
	if resp.Summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if len(resp.ReferencedEvents) == 0 {
		t.Fatal("expected referenced events")
	}
}

func TestComposePipelineFakeProvider(t *testing.T) {
	pipeCfg := PipelineConfig{
		AIMaxPromptChars:         40000,
		AIMaxCompletionTokens:    2000,
		AIMaxEventsPerRequest:    50,
		AIRetryMaxAttempts:       3,
		AIRetryBaseBackoffMs:     1000,
		AIRetryMaxBackoffMs:      10000,
		AIRatePerSecond:          10,
		AIRateBurst:              5,
		AICostPerPromptToken:     0.00015,
		AICostPerCompletionToken: 0.00060,
		AIModel:                  "gpt-4o-mini",
		AIProvider:               "fake",
	}

	svc := ComposePipeline(pipeCfg, nil)

	req := validGatewayRequest(t, GatewayModeEngineer)
	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success with explicit fake provider: %v", err)
	}
	if resp.Status != StatusSuccess {
		t.Fatalf("expected success status, got %s", resp.Status)
	}
}

func TestComposePipelineOpenRouterProviderUsesAdapter(t *testing.T) {
	pipeCfg := PipelineConfig{
		AIMaxPromptChars:         40000,
		AIMaxCompletionTokens:    2000,
		AIMaxEventsPerRequest:    50,
		AIRetryMaxAttempts:       1,
		AIRetryBaseBackoffMs:     100,
		AIRetryMaxBackoffMs:      100,
		AIRatePerSecond:          1000,
		AIRateBurst:              100,
		AICostPerPromptToken:     0.00015,
		AICostPerCompletionToken: 0.00060,
		AIModel:                  "gpt-4o-mini",
		AIProvider:               "openrouter",
		OpenRouterAPIKey:         "test-key",
		OpenRouterBaseURL:        "http://127.0.0.1:1",
	}

	svc := ComposePipeline(pipeCfg, nil)

	req := validGatewayRequest(t, GatewayModeEngineer)
	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback, not error: %v", err)
	}
	if resp.Status != StatusProviderError {
		t.Fatalf("expected provider_error for unreachable openrouter, got %s", resp.Status)
	}
}

func TestComposePipelineWithAllConfigDerivedFromConfig(t *testing.T) {
	cfg := validConfigForPipeline()
	pipeCfg := PipelineConfigFromConfig(cfg)

	if pipeCfg.AIProvider != "" {
		t.Fatalf("expected empty AIProvider from zero config, got %q", pipeCfg.AIProvider)
	}
	if pipeCfg.AIModel != "" {
		t.Fatalf("expected empty AIModel from zero config, got %q", pipeCfg.AIModel)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := ComposePipeline(pipeCfg, logger)

	req := validGatewayRequest(t, GatewayModeEngineer)
	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success with zero config defaults: %v", err)
	}
	if resp.Summary == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestComposePipelineCascadesFallbackOnProviderError(t *testing.T) {
	pipeCfg := PipelineConfig{
		AIMaxPromptChars:         40000,
		AIMaxCompletionTokens:    2000,
		AIMaxEventsPerRequest:    50,
		AIRetryMaxAttempts:       1,
		AIRetryBaseBackoffMs:     100,
		AIRetryMaxBackoffMs:      100,
		AIRatePerSecond:          10,
		AIRateBurst:              5,
		AICostPerPromptToken:     0.00015,
		AICostPerCompletionToken: 0.00060,
		AIModel:                  "gpt-4o-mini",
		AIProvider:               "openrouter",
		OpenRouterAPIKey:         "invalid",
		OpenRouterBaseURL:        "http://127.0.0.1:1",
	}

	svc := ComposePipeline(pipeCfg, nil)

	req := validGatewayRequest(t, GatewayModeEngineer)
	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected fallback response, not error: %v", err)
	}
	if resp.Status != StatusProviderError {
		t.Fatalf("expected provider_error status for unreachable openrouter, got %s", resp.Status)
	}
	if !strings.Contains(resp.Summary, "AI analysis is currently unavailable") {
		t.Fatalf("expected fallback summary, got: %s", resp.Summary)
	}
}

func TestFakeProviderSatisfiesProviderAdapter(t *testing.T) {
	var adapter ProviderAdapter = &FakeProvider{}

	resp, err := adapter.Analyze(context.Background(), ProviderRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: "user", Content: "test input"},
		},
		Temperature: 0.7,
		MaxTokens:   500,
	})
	if err != nil {
		t.Fatalf("expected fake provider to succeed: %v", err)
	}
	if resp.Content == "" {
		t.Fatalf("expected non-empty response")
	}
	if resp.Model != "test-model" {
		t.Fatalf("expected model test-model, got %s", resp.Model)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("expected finish reason stop, got %s", resp.FinishReason)
	}
}

func validConfigForPipeline() config.Config {
	return config.Config{}
}
