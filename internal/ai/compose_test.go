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
	if !strings.Contains(resp.Summary, "structured engineer events") {
		t.Fatalf("expected fake engineer summary to mention structured engineer events, got %q", resp.Summary)
	}
	if strings.Contains(resp.Summary, "Telemetry One Engineer") || strings.Contains(resp.Summary, "Session:") {
		t.Fatalf("expected fake engineer summary not to echo raw prompt, got %q", resp.Summary)
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

func TestFakeProviderReturnsEngineerAdviceWithoutPromptEcho(t *testing.T) {
	adapter := &FakeProvider{}
	inputPrompt := "Telemetry One Engineer\nSession: session-1\nEngineer Events (1 total): event-1"

	resp, err := adapter.Analyze(context.Background(), ProviderRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: "system", Content: inputPrompt},
			{Role: "user", Content: "Analyze structured engineer events for live advice."},
		},
		Temperature: 0.7,
		MaxTokens:   500,
	})
	if err != nil {
		t.Fatalf("expected fake provider to succeed: %v", err)
	}
	if resp.Content == "" || !strings.Contains(resp.Content, "Fake race engineer analysis") {
		t.Fatalf("expected presentable fake race engineer content, got %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "structured engineer events") || !strings.Contains(resp.Content, "actionable") && !strings.Contains(resp.Content, "Prioritize") {
		t.Fatalf("expected fake content to mention structured events and actionable advice, got %q", resp.Content)
	}
	if strings.Contains(resp.Content, inputPrompt) || strings.Contains(resp.Content, "Session: session-1") {
		t.Fatalf("expected fake content not to echo raw prompt, got %q", resp.Content)
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

func TestPipelineConfigValidateOpenRouterWithoutKey(t *testing.T) {
	cfg := PipelineConfig{
		AIProvider: "openrouter",
	}
	warnings := cfg.Validate()
	if len(warnings) == 0 {
		t.Fatal("expected validation warning for openrouter without API key")
	}
	if warnings[0].Field != "OpenRouterAPIKey" {
		t.Fatalf("expected field OpenRouterAPIKey, got %q", warnings[0].Field)
	}
}

func TestPipelineConfigValidateOpenRouterWithKey(t *testing.T) {
	cfg := PipelineConfig{
		AIProvider:       "openrouter",
		OpenRouterAPIKey: "sk-or-v1-test",
	}
	warnings := cfg.Validate()
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for openrouter with key, got %v", warnings)
	}
}

func TestPipelineConfigValidateFakeProvider(t *testing.T) {
	cfg := PipelineConfig{
		AIProvider: "fake",
	}
	warnings := cfg.Validate()
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for fake provider, got %v", warnings)
	}
}

func TestPipelineConfigValidateEmptyProvider(t *testing.T) {
	cfg := PipelineConfig{}
	warnings := cfg.Validate()
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for empty provider, got %v", warnings)
	}
}

func validConfigForPipeline() config.Config {
	return config.Config{}
}
