package ai

import (
	"context"
	"log/slog"

	"telemetry-one-backend/internal/config"
)

type FakeProvider struct{}

func (f *FakeProvider) Analyze(_ context.Context, req ProviderRequest) (ProviderResponse, error) {
	content := "AI analysis result"
	if len(req.Messages) > 0 {
		content = "AI analysis result for " + req.Messages[0].Content
	}
	return ProviderResponse{
		Content:      content,
		Model:        req.Model,
		FinishReason: "stop",
		Usage:        UsageInfo{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}, nil
}

var _ ProviderAdapter = (*FakeProvider)(nil)

type PipelineConfig struct {
	AIMaxPromptChars         int
	AIMaxCompletionTokens    int
	AIMaxEventsPerRequest    int
	AIRetryMaxAttempts       int
	AIRetryBaseBackoffMs     int
	AIRetryMaxBackoffMs      int
	AIRatePerSecond          float64
	AIRateBurst              int
	AICostPerPromptToken     float64
	AICostPerCompletionToken float64
	AIModel                  string
	AIProvider               string
	OpenRouterAPIKey         string
	OpenRouterBaseURL        string
	OpenRouterHTTPReferer    string
	OpenRouterTitle          string
}

func PipelineConfigFromConfig(cfg config.Config) PipelineConfig {
	return PipelineConfig{
		AIMaxPromptChars:         cfg.AIMaxPromptChars,
		AIMaxCompletionTokens:    cfg.AIMaxCompletionTokens,
		AIMaxEventsPerRequest:    cfg.AIMaxEventsPerRequest,
		AIRetryMaxAttempts:       cfg.AIRetryMaxAttempts,
		AIRetryBaseBackoffMs:     cfg.AIRetryBaseBackoffMs,
		AIRetryMaxBackoffMs:      cfg.AIRetryMaxBackoffMs,
		AIRatePerSecond:          cfg.AIRatePerSecond,
		AIRateBurst:              cfg.AIRateBurst,
		AICostPerPromptToken:     cfg.AICostPerPromptToken,
		AICostPerCompletionToken: cfg.AICostPerCompletionToken,
		AIModel:                  cfg.AIModel,
		AIProvider:               cfg.AIProvider,
		OpenRouterAPIKey:         cfg.OpenRouterAPIKey,
		OpenRouterBaseURL:        cfg.OpenRouterBaseURL,
		OpenRouterHTTPReferer:    cfg.OpenRouterHTTPReferer,
		OpenRouterTitle:          cfg.OpenRouterTitle,
	}
}

func ComposePipeline(pipeCfg PipelineConfig, logger *slog.Logger) AIService {
	return ComposePipelineWithAudit(pipeCfg, logger, NewMemoryAuditStore())
}

func ComposePipelineWithAudit(pipeCfg PipelineConfig, logger *slog.Logger, auditStore AuditLogger) AIService {
	if auditStore == nil {
		auditStore = NewMemoryAuditStore()
	}

	budget := TokenBudget{
		MaxPromptChars:      pipeCfg.AIMaxPromptChars,
		MaxCompletionTokens: pipeCfg.AIMaxCompletionTokens,
		MaxEventsPerRequest: pipeCfg.AIMaxEventsPerRequest,
	}
	retry := RetryPolicy{
		MaxAttempts:   pipeCfg.AIRetryMaxAttempts,
		BaseBackoffMs: pipeCfg.AIRetryBaseBackoffMs,
		MaxBackoffMs:  pipeCfg.AIRetryMaxBackoffMs,
	}

	controller := &Controller{
		Budget:      budget,
		Retry:       retry,
		RateLimiter: NewRateLimiter(pipeCfg.AIRatePerSecond, pipeCfg.AIRateBurst),
		Account:     NewUsageAccount(),
		CostEstimator: CostEstimator{
			PerPromptToken:     pipeCfg.AICostPerPromptToken,
			PerCompletionToken: pipeCfg.AICostPerCompletionToken,
		},
	}

	var adapter ProviderAdapter
	switch pipeCfg.AIProvider {
	case "openrouter":
		adapter = NewOpenRouterAdapter(OpenRouterConfig{
			APIKey:      pipeCfg.OpenRouterAPIKey,
			BaseURL:     pipeCfg.OpenRouterBaseURL,
			HTTPReferer: pipeCfg.OpenRouterHTTPReferer,
			Title:       pipeCfg.OpenRouterTitle,
		})
		if logger != nil {
			logger.Info("ai pipeline configured",
				"provider", "openrouter",
				"model", pipeCfg.AIModel,
				"base_url", pipeCfg.OpenRouterBaseURL,
			)
		}
	default:
		adapter = &FakeProvider{}
		if logger != nil {
			logger.Info("ai pipeline configured",
				"provider", "fake",
				"model", pipeCfg.AIModel,
			)
		}
	}

	svc := NewService(adapter, controller).WithModel(pipeCfg.AIModel)
	fallbackSvc := NewFallbackService(svc)
	return NewAuditedService(fallbackSvc, auditStore, nil)
}
