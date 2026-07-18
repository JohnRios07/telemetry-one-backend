package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	DefaultAddr                     = ":8080"
	DefaultEnv                      = "development"
	DefaultLogLevel                 = "info"
	DefaultReadHeaderTimeout        = 5 * time.Second
	DefaultShutdownTimeout          = 10 * time.Second
	DefaultRetainedFramesPerSession = 12000
	DefaultDatabaseURL              = ""

	DefaultAIMaxPromptChars            = 40000
	DefaultAIMaxCompletionTokens       = 2000
	DefaultAIMaxEventsPerRequest       = 50
	DefaultAIRetryMaxAttempts          = 3
	DefaultAIRetryBaseBackoffMs        = 1000
	DefaultAIRetryMaxBackoffMs         = 10000
	DefaultAIRatePerSecond             = 10.0
	DefaultAIRateBurst                 = 5
	DefaultAICostPerPromptToken        = 0.00015
	DefaultAICostPerCompletionToken    = 0.00060
	DefaultAIModel                     = "gpt-4o-mini"
	DefaultAIProvider                  = "fake"
	DefaultOpenRouterBaseURL           = "https://openrouter.ai/api/v1"
	DefaultSkipInvalidApprovedGeometry = false
)

type Config struct {
	Addr                        string
	Env                         string
	LogLevel                    string
	ReadHeaderTimeout           time.Duration
	ShutdownTimeout             time.Duration
	RetainedFramesPerSession    int
	DatabaseURL                 string
	AdminToken                  string
	SkipInvalidApprovedGeometry bool

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

	AIProvider            string
	OpenRouterAPIKey      string
	OpenRouterBaseURL     string
	OpenRouterHTTPReferer string
	OpenRouterTitle       string
}

func Load() (Config, error) {
	readHeaderTimeout, err := durationEnv("TELEMETRY_ONE_READ_HEADER_TIMEOUT", DefaultReadHeaderTimeout)
	if err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := durationEnv("TELEMETRY_ONE_SHUTDOWN_TIMEOUT", DefaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}

	retainedFramesPerSession, err := positiveIntEnv("TELEMETRY_ONE_RETAINED_FRAMES_PER_SESSION", DefaultRetainedFramesPerSession)
	if err != nil {
		return Config{}, err
	}

	aiMaxPromptChars, err := positiveIntEnv("TELEMETRY_ONE_AI_MAX_PROMPT_CHARS", DefaultAIMaxPromptChars)
	if err != nil {
		return Config{}, err
	}
	aiMaxCompletionTokens, err := positiveIntEnv("TELEMETRY_ONE_AI_MAX_COMPLETION_TOKENS", DefaultAIMaxCompletionTokens)
	if err != nil {
		return Config{}, err
	}
	aiMaxEventsPerRequest, err := positiveIntEnv("TELEMETRY_ONE_AI_MAX_EVENTS_PER_REQUEST", DefaultAIMaxEventsPerRequest)
	if err != nil {
		return Config{}, err
	}
	aiRetryMaxAttempts, err := positiveIntEnv("TELEMETRY_ONE_AI_RETRY_MAX_ATTEMPTS", DefaultAIRetryMaxAttempts)
	if err != nil {
		return Config{}, err
	}
	aiRetryBaseBackoffMs, err := positiveIntEnv("TELEMETRY_ONE_AI_RETRY_BASE_BACKOFF_MS", DefaultAIRetryBaseBackoffMs)
	if err != nil {
		return Config{}, err
	}
	aiRetryMaxBackoffMs, err := positiveIntEnv("TELEMETRY_ONE_AI_RETRY_MAX_BACKOFF_MS", DefaultAIRetryMaxBackoffMs)
	if err != nil {
		return Config{}, err
	}
	aiRatePerSecond, err := positiveFloat64Env("TELEMETRY_ONE_AI_RATE_PER_SECOND", DefaultAIRatePerSecond)
	if err != nil {
		return Config{}, err
	}
	aiRateBurst, err := positiveIntEnv("TELEMETRY_ONE_AI_RATE_BURST", DefaultAIRateBurst)
	if err != nil {
		return Config{}, err
	}
	aiCostPerPromptToken, err := positiveFloat64Env("TELEMETRY_ONE_AI_COST_PER_PROMPT_TOKEN", DefaultAICostPerPromptToken)
	if err != nil {
		return Config{}, err
	}
	aiCostPerCompletionToken, err := positiveFloat64Env("TELEMETRY_ONE_AI_COST_PER_COMPLETION_TOKEN", DefaultAICostPerCompletionToken)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Addr:                        stringEnv("TELEMETRY_ONE_ADDR", DefaultAddr),
		Env:                         stringEnv("TELEMETRY_ONE_ENV", DefaultEnv),
		LogLevel:                    stringEnv("TELEMETRY_ONE_LOG_LEVEL", DefaultLogLevel),
		ReadHeaderTimeout:           readHeaderTimeout,
		ShutdownTimeout:             shutdownTimeout,
		RetainedFramesPerSession:    retainedFramesPerSession,
		DatabaseURL:                 stringEnv("TELEMETRY_ONE_DATABASE_URL", DefaultDatabaseURL),
		SkipInvalidApprovedGeometry: boolEnv("TELEMETRY_ONE_SKIP_INVALID_APPROVED_GEOMETRY", DefaultSkipInvalidApprovedGeometry),

		AIMaxPromptChars:         aiMaxPromptChars,
		AIMaxCompletionTokens:    aiMaxCompletionTokens,
		AIMaxEventsPerRequest:    aiMaxEventsPerRequest,
		AIRetryMaxAttempts:       aiRetryMaxAttempts,
		AIRetryBaseBackoffMs:     aiRetryBaseBackoffMs,
		AIRetryMaxBackoffMs:      aiRetryMaxBackoffMs,
		AIRatePerSecond:          aiRatePerSecond,
		AIRateBurst:              aiRateBurst,
		AICostPerPromptToken:     aiCostPerPromptToken,
		AICostPerCompletionToken: aiCostPerCompletionToken,
		AIModel:                  stringEnv("TELEMETRY_ONE_AI_MODEL", DefaultAIModel),
		AdminToken:               os.Getenv("TELEMETRY_ONE_ADMIN_TOKEN"),

		AIProvider:            stringEnv("TELEMETRY_ONE_AI_PROVIDER", DefaultAIProvider),
		OpenRouterAPIKey:      stringEnv("TELEMETRY_ONE_OPENROUTER_API_KEY", ""),
		OpenRouterBaseURL:     stringEnv("TELEMETRY_ONE_OPENROUTER_BASE_URL", DefaultOpenRouterBaseURL),
		OpenRouterHTTPReferer: stringEnv("TELEMETRY_ONE_OPENROUTER_HTTP_REFERER", ""),
		OpenRouterTitle:       stringEnv("TELEMETRY_ONE_OPENROUTER_TITLE", ""),
	}, nil
}

func stringEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s duration %q: %w", key, value, err)
	}

	return duration, nil
}

func positiveFloat64Env(key string, fallback float64) (float64, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s float %q: %w", key, value, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("invalid %s float %q: must be greater than zero", key, value)
	}

	return parsed, nil
}

func positiveIntEnv(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s integer %q: %w", key, value, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("invalid %s integer %q: must be greater than zero", key, value)
	}

	return parsed, nil
}

func boolEnv(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}

	return parsed
}
