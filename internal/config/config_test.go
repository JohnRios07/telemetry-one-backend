package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("TELEMETRY_ONE_ADDR", "")
	t.Setenv("TELEMETRY_ONE_ENV", "")
	t.Setenv("TELEMETRY_ONE_LOG_LEVEL", "")
	t.Setenv("TELEMETRY_ONE_READ_HEADER_TIMEOUT", "")
	t.Setenv("TELEMETRY_ONE_SHUTDOWN_TIMEOUT", "")
	t.Setenv("TELEMETRY_ONE_RETAINED_FRAMES_PER_SESSION", "")
	t.Setenv("TELEMETRY_ONE_DATABASE_URL", "")
	t.Setenv("TELEMETRY_ONE_AI_MAX_PROMPT_CHARS", "")
	t.Setenv("TELEMETRY_ONE_AI_MAX_COMPLETION_TOKENS", "")
	t.Setenv("TELEMETRY_ONE_AI_MAX_EVENTS_PER_REQUEST", "")
	t.Setenv("TELEMETRY_ONE_AI_RETRY_MAX_ATTEMPTS", "")
	t.Setenv("TELEMETRY_ONE_AI_RETRY_BASE_BACKOFF_MS", "")
	t.Setenv("TELEMETRY_ONE_AI_RETRY_MAX_BACKOFF_MS", "")
	t.Setenv("TELEMETRY_ONE_AI_RATE_PER_SECOND", "")
	t.Setenv("TELEMETRY_ONE_AI_RATE_BURST", "")
	t.Setenv("TELEMETRY_ONE_AI_COST_PER_PROMPT_TOKEN", "")
	t.Setenv("TELEMETRY_ONE_AI_COST_PER_COMPLETION_TOKEN", "")
	t.Setenv("TELEMETRY_ONE_AI_MODEL", "")
	t.Setenv("TELEMETRY_ONE_AI_PROVIDER", "")
	t.Setenv("TELEMETRY_ONE_OPENROUTER_API_KEY", "")
	t.Setenv("TELEMETRY_ONE_OPENROUTER_BASE_URL", "")
	t.Setenv("TELEMETRY_ONE_OPENROUTER_HTTP_REFERER", "")
	t.Setenv("TELEMETRY_ONE_OPENROUTER_TITLE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected defaults to load without error: %v", err)
	}

	if cfg.Addr != DefaultAddr {
		t.Fatalf("expected addr %q, got %q", DefaultAddr, cfg.Addr)
	}
	if cfg.Env != DefaultEnv {
		t.Fatalf("expected env %q, got %q", DefaultEnv, cfg.Env)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Fatalf("expected log level %q, got %q", DefaultLogLevel, cfg.LogLevel)
	}
	if cfg.ReadHeaderTimeout != DefaultReadHeaderTimeout {
		t.Fatalf("expected read header timeout %s, got %s", DefaultReadHeaderTimeout, cfg.ReadHeaderTimeout)
	}
	if cfg.ShutdownTimeout != DefaultShutdownTimeout {
		t.Fatalf("expected shutdown timeout %s, got %s", DefaultShutdownTimeout, cfg.ShutdownTimeout)
	}
	if cfg.RetainedFramesPerSession != DefaultRetainedFramesPerSession {
		t.Fatalf("expected retained frames per session %d, got %d", DefaultRetainedFramesPerSession, cfg.RetainedFramesPerSession)
	}
	if cfg.DatabaseURL != DefaultDatabaseURL {
		t.Fatalf("expected database URL %q, got %q", DefaultDatabaseURL, cfg.DatabaseURL)
	}
	if cfg.AIMaxPromptChars != DefaultAIMaxPromptChars {
		t.Fatalf("expected AI max prompt chars %d, got %d", DefaultAIMaxPromptChars, cfg.AIMaxPromptChars)
	}
	if cfg.AIMaxCompletionTokens != DefaultAIMaxCompletionTokens {
		t.Fatalf("expected AI max completion tokens %d, got %d", DefaultAIMaxCompletionTokens, cfg.AIMaxCompletionTokens)
	}
	if cfg.AIMaxEventsPerRequest != DefaultAIMaxEventsPerRequest {
		t.Fatalf("expected AI max events per request %d, got %d", DefaultAIMaxEventsPerRequest, cfg.AIMaxEventsPerRequest)
	}
	if cfg.AIRetryMaxAttempts != DefaultAIRetryMaxAttempts {
		t.Fatalf("expected AI retry max attempts %d, got %d", DefaultAIRetryMaxAttempts, cfg.AIRetryMaxAttempts)
	}
	if cfg.AIRetryBaseBackoffMs != DefaultAIRetryBaseBackoffMs {
		t.Fatalf("expected AI retry base backoff ms %d, got %d", DefaultAIRetryBaseBackoffMs, cfg.AIRetryBaseBackoffMs)
	}
	if cfg.AIRetryMaxBackoffMs != DefaultAIRetryMaxBackoffMs {
		t.Fatalf("expected AI retry max backoff ms %d, got %d", DefaultAIRetryMaxBackoffMs, cfg.AIRetryMaxBackoffMs)
	}
	if cfg.AIRatePerSecond != DefaultAIRatePerSecond {
		t.Fatalf("expected AI rate per second %.4f, got %.4f", DefaultAIRatePerSecond, cfg.AIRatePerSecond)
	}
	if cfg.AIRateBurst != DefaultAIRateBurst {
		t.Fatalf("expected AI rate burst %d, got %d", DefaultAIRateBurst, cfg.AIRateBurst)
	}
	if cfg.AICostPerPromptToken != DefaultAICostPerPromptToken {
		t.Fatalf("expected AI cost per prompt token %.6f, got %.6f", DefaultAICostPerPromptToken, cfg.AICostPerPromptToken)
	}
	if cfg.AICostPerCompletionToken != DefaultAICostPerCompletionToken {
		t.Fatalf("expected AI cost per completion token %.6f, got %.6f", DefaultAICostPerCompletionToken, cfg.AICostPerCompletionToken)
	}
	if cfg.AIModel != DefaultAIModel {
		t.Fatalf("expected AI model %q, got %q", DefaultAIModel, cfg.AIModel)
	}
	if cfg.AIProvider != DefaultAIProvider {
		t.Fatalf("expected AI provider %q, got %q", DefaultAIProvider, cfg.AIProvider)
	}
	if cfg.OpenRouterAPIKey != "" {
		t.Fatalf("expected empty OpenRouter API key, got %q", cfg.OpenRouterAPIKey)
	}
	if cfg.OpenRouterBaseURL != DefaultOpenRouterBaseURL {
		t.Fatalf("expected OpenRouter base URL %q, got %q", DefaultOpenRouterBaseURL, cfg.OpenRouterBaseURL)
	}
	if cfg.OpenRouterHTTPReferer != "" {
		t.Fatalf("expected empty HTTP-Referer, got %q", cfg.OpenRouterHTTPReferer)
	}
	if cfg.OpenRouterTitle != "" {
		t.Fatalf("expected empty Title, got %q", cfg.OpenRouterTitle)
	}
}

func TestLoadEnvironmentOverrides(t *testing.T) {
	t.Setenv("TELEMETRY_ONE_ADDR", ":9090")
	t.Setenv("TELEMETRY_ONE_ENV", "test")
	t.Setenv("TELEMETRY_ONE_LOG_LEVEL", "debug")
	t.Setenv("TELEMETRY_ONE_READ_HEADER_TIMEOUT", "2s")
	t.Setenv("TELEMETRY_ONE_SHUTDOWN_TIMEOUT", "3s")
	t.Setenv("TELEMETRY_ONE_RETAINED_FRAMES_PER_SESSION", "42")
	t.Setenv("TELEMETRY_ONE_DATABASE_URL", "postgres://telemetry:secret@localhost:5432/telemetry_one?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected env overrides to load without error: %v", err)
	}

	if cfg.Addr != ":9090" {
		t.Fatalf("expected addr override, got %q", cfg.Addr)
	}
	if cfg.Env != "test" {
		t.Fatalf("expected env override, got %q", cfg.Env)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("expected log level override, got %q", cfg.LogLevel)
	}
	if cfg.ReadHeaderTimeout != 2*time.Second {
		t.Fatalf("expected read header timeout override, got %s", cfg.ReadHeaderTimeout)
	}
	if cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("expected shutdown timeout override, got %s", cfg.ShutdownTimeout)
	}
	if cfg.RetainedFramesPerSession != 42 {
		t.Fatalf("expected retained frames per session override, got %d", cfg.RetainedFramesPerSession)
	}
	if cfg.DatabaseURL != "postgres://telemetry:secret@localhost:5432/telemetry_one?sslmode=disable" {
		t.Fatalf("expected database URL override, got %q", cfg.DatabaseURL)
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	t.Setenv("TELEMETRY_ONE_READ_HEADER_TIMEOUT", "soon")

	if _, err := Load(); err == nil {
		t.Fatal("expected invalid duration to return an error")
	}
}

func TestLoadAIEnvironmentOverrides(t *testing.T) {
	t.Setenv("TELEMETRY_ONE_AI_MAX_PROMPT_CHARS", "10000")
	t.Setenv("TELEMETRY_ONE_AI_MAX_COMPLETION_TOKENS", "500")
	t.Setenv("TELEMETRY_ONE_AI_MAX_EVENTS_PER_REQUEST", "20")
	t.Setenv("TELEMETRY_ONE_AI_RETRY_MAX_ATTEMPTS", "5")
	t.Setenv("TELEMETRY_ONE_AI_RETRY_BASE_BACKOFF_MS", "500")
	t.Setenv("TELEMETRY_ONE_AI_RETRY_MAX_BACKOFF_MS", "3000")
	t.Setenv("TELEMETRY_ONE_AI_RATE_PER_SECOND", "20.5")
	t.Setenv("TELEMETRY_ONE_AI_RATE_BURST", "10")
	t.Setenv("TELEMETRY_ONE_AI_COST_PER_PROMPT_TOKEN", "0.0002")
	t.Setenv("TELEMETRY_ONE_AI_COST_PER_COMPLETION_TOKEN", "0.0008")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected AI overrides to load: %v", err)
	}

	if cfg.AIMaxPromptChars != 10000 {
		t.Fatalf("expected AI max prompt chars 10000, got %d", cfg.AIMaxPromptChars)
	}
	if cfg.AIMaxCompletionTokens != 500 {
		t.Fatalf("expected AI max completion tokens 500, got %d", cfg.AIMaxCompletionTokens)
	}
	if cfg.AIMaxEventsPerRequest != 20 {
		t.Fatalf("expected AI max events per request 20, got %d", cfg.AIMaxEventsPerRequest)
	}
	if cfg.AIRetryMaxAttempts != 5 {
		t.Fatalf("expected AI retry max attempts 5, got %d", cfg.AIRetryMaxAttempts)
	}
	if cfg.AIRetryBaseBackoffMs != 500 {
		t.Fatalf("expected AI retry base backoff ms 500, got %d", cfg.AIRetryBaseBackoffMs)
	}
	if cfg.AIRetryMaxBackoffMs != 3000 {
		t.Fatalf("expected AI retry max backoff ms 3000, got %d", cfg.AIRetryMaxBackoffMs)
	}
	if cfg.AIRatePerSecond != 20.5 {
		t.Fatalf("expected AI rate per second 20.5, got %.4f", cfg.AIRatePerSecond)
	}
	if cfg.AIRateBurst != 10 {
		t.Fatalf("expected AI rate burst 10, got %d", cfg.AIRateBurst)
	}
	if cfg.AICostPerPromptToken != 0.0002 {
		t.Fatalf("expected AI cost per prompt token 0.0002, got %.6f", cfg.AICostPerPromptToken)
	}
	if cfg.AICostPerCompletionToken != 0.0008 {
		t.Fatalf("expected AI cost per completion token 0.0008, got %.6f", cfg.AICostPerCompletionToken)
	}
}

func TestLoadRejectsInvalidAIFloat(t *testing.T) {
	t.Setenv("TELEMETRY_ONE_AI_RATE_PER_SECOND", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid AI float to return an error")
	}
}

func TestLoadRejectsInvalidAIFloatZero(t *testing.T) {
	t.Setenv("TELEMETRY_ONE_AI_COST_PER_PROMPT_TOKEN", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected zero AI float to return an error")
	}
}

func TestLoadAIModelOverride(t *testing.T) {
	t.Setenv("TELEMETRY_ONE_AI_MODEL", "custom-model")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected AI model override to load: %v", err)
	}
	if cfg.AIModel != "custom-model" {
		t.Fatalf("expected AI model 'custom-model', got %q", cfg.AIModel)
	}
}

func TestLoadAIProviderOverride(t *testing.T) {
	t.Setenv("TELEMETRY_ONE_AI_PROVIDER", "openrouter")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected AI provider override to load: %v", err)
	}
	if cfg.AIProvider != "openrouter" {
		t.Fatalf("expected AI provider 'openrouter', got %q", cfg.AIProvider)
	}
}

func TestLoadOpenRouterAPIKeyOverride(t *testing.T) {
	t.Setenv("TELEMETRY_ONE_OPENROUTER_API_KEY", "sk-or-v1-test-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected OpenRouter API key override to load: %v", err)
	}
	if cfg.OpenRouterAPIKey != "sk-or-v1-test-key" {
		t.Fatalf("expected API key override, got %q", cfg.OpenRouterAPIKey)
	}
}

func TestLoadOpenRouterBaseURLOverride(t *testing.T) {
	t.Setenv("TELEMETRY_ONE_OPENROUTER_BASE_URL", "http://localhost:8080")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected OpenRouter base URL override to load: %v", err)
	}
	if cfg.OpenRouterBaseURL != "http://localhost:8080" {
		t.Fatalf("expected base URL override, got %q", cfg.OpenRouterBaseURL)
	}
}

func TestLoadOpenRouterOptionalHeaders(t *testing.T) {
	t.Setenv("TELEMETRY_ONE_OPENROUTER_HTTP_REFERER", "https://telemetryone.app")
	t.Setenv("TELEMETRY_ONE_OPENROUTER_TITLE", "Telemetry One")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected OpenRouter optional headers to load: %v", err)
	}
	if cfg.OpenRouterHTTPReferer != "https://telemetryone.app" {
		t.Fatalf("expected HTTP-Referer override, got %q", cfg.OpenRouterHTTPReferer)
	}
	if cfg.OpenRouterTitle != "Telemetry One" {
		t.Fatalf("expected Title override, got %q", cfg.OpenRouterTitle)
	}
}

func TestLoadRejectsInvalidRetainedFramesPerSession(t *testing.T) {
	t.Setenv("TELEMETRY_ONE_RETAINED_FRAMES_PER_SESSION", "0")

	if _, err := Load(); err == nil {
		t.Fatal("expected invalid retained frames per session to return an error")
	}
}
