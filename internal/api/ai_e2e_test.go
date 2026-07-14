package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"telemetry-one-backend/internal/admin"
	"telemetry-one-backend/internal/ai"
	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

func testAIHandler(t *testing.T, cfg config.Config, frameStore telemetry.Store, catalog tracks.Catalog, eventStore events.Repository, aiSvc ai.AIService) http.Handler {
	t.Helper()
	sessionRepo := sessions.NewMemoryRepository()
	if _, err := sessionRepo.Create(context.Background(), sessions.Session{
		ID: "test-session", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: time.UnixMilli(1720656000000).UTC(),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	statsRepo := admin.NewMemoryStatsRepo(sessionRepo, frameStore)
	return routesWithAIAndSessions(cfg, logger, frameStore, catalog, eventStore, sessionRepo, aiSvc, statsRepo)
}

func TestAIE2E_OpenRouterSuccessPath(t *testing.T) {
	var captured struct {
		body        []byte
		authHeader  string
		contentType string
		hasReferer  bool
		hasTitle    bool
		reqPath     string
	}
	mockOR := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.reqPath = r.URL.Path
		captured.body, _ = io.ReadAll(r.Body)
		captured.authHeader = r.Header.Get("Authorization")
		captured.contentType = r.Header.Get("Content-Type")
		captured.hasReferer = r.Header.Get("HTTP-Referer") != ""
		captured.hasTitle = r.Header.Get("X-OpenRouter-Title") != ""

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-e2e-success",
			"model": "gpt-4o-mini",
			"choices": []map[string]any{
				{
					"message":       map[string]any{"content": "E2E analysis: consistent braking pattern found."},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     150,
				"completion_tokens": 200,
				"total_tokens":      350,
			},
		})
	}))
	defer mockOR.Close()

	cfg := config.Config{Addr: ":0", Env: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	eventStore := events.NewStore(100, events.DedupOptions{})

	pipeCfg := ai.PipelineConfig{
		AIMaxPromptChars:         40000,
		AIMaxCompletionTokens:    2000,
		AIMaxEventsPerRequest:    50,
		AIRetryMaxAttempts:       2,
		AIRetryBaseBackoffMs:     5,
		AIRetryMaxBackoffMs:      10,
		AIRatePerSecond:          1000,
		AIRateBurst:              100,
		AICostPerPromptToken:     0.00015,
		AICostPerCompletionToken: 0.00060,
		AIModel:                  "gpt-4o-mini",
		AIProvider:               "openrouter",
		OpenRouterAPIKey:         "sk-e2e-test-key",
		OpenRouterBaseURL:        mockOR.URL,
	}
	aiSvc := ai.ComposePipeline(pipeCfg, logger)

	handler := testAIHandler(t, cfg, frameStore, catalog, eventStore, aiSvc)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test-session/analyze", strings.NewReader(validAnalyzePayload))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	if captured.reqPath != "/api/v1/chat/completions" {
		t.Fatalf("expected /api/v1/chat/completions, got %q", captured.reqPath)
	}
	if captured.authHeader != "Bearer sk-e2e-test-key" {
		t.Fatalf("expected Bearer sk-e2e-test-key, got %q", captured.authHeader)
	}
	if captured.contentType != "application/json" {
		t.Fatalf("expected application/json, got %q", captured.contentType)
	}
	if captured.hasReferer {
		t.Fatal("expected no HTTP-Referer when not configured")
	}
	if captured.hasTitle {
		t.Fatal("expected no X-OpenRouter-Title when not configured")
	}

	var outgoing struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(captured.body, &outgoing); err != nil {
		t.Fatalf("failed to decode outgoing request: %v", err)
	}
	if outgoing.Model != "gpt-4o-mini" {
		t.Fatalf("expected model gpt-4o-mini, got %q", outgoing.Model)
	}
	if len(outgoing.Messages) < 2 {
		t.Fatalf("expected at least 2 messages (system+user), got %d", len(outgoing.Messages))
	}
	if outgoing.Messages[0].Role != "system" {
		t.Fatalf("expected first message role system, got %q", outgoing.Messages[0].Role)
	}
	if outgoing.Messages[1].Role != "user" {
		t.Fatalf("expected second message role user, got %q", outgoing.Messages[1].Role)
	}

	bodyStr := string(captured.body)
	for _, forbidden := range []string{`"positionX":`, `"positionY":`, `"positionZ":`, `"speedMps":`, `"rpm":`, `"gear":`, `"throttle":`, `"brake":`, `"steering":`, `"fuelLiters":`, `"wheelSpeedFL":`, `"wheelSpeedFR":`, `"wheelSpeedRL":`, `"wheelSpeedRR":`, `"isOnTrack":`, `"frames":`, `"telemetryFrames":`} {
		if strings.Contains(bodyStr, forbidden) {
			t.Fatalf("outgoing request must not contain raw telemetry field %q", forbidden)
		}
	}

	var response ai.GatewayResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Status != ai.StatusSuccess {
		t.Fatalf("expected success, got %q", response.Status)
	}
	if response.Summary != "E2E analysis: consistent braking pattern found." {
		t.Fatalf("unexpected summary: %q", response.Summary)
	}
	if len(response.ReferencedEvents) == 0 {
		t.Fatal("expected referenced events")
	}
	if response.ReferencedEvents[0] != "event-1" {
		t.Fatalf("expected referenced event event-1, got %v", response.ReferencedEvents)
	}
	if response.ProviderInfo.Model != "gpt-4o-mini" {
		t.Fatalf("expected model gpt-4o-mini, got %q", response.ProviderInfo.Model)
	}
	if response.ProviderInfo.FinishReason != "stop" {
		t.Fatalf("expected finish_reason stop, got %q", response.ProviderInfo.FinishReason)
	}
	if response.ProviderInfo.Usage.TotalTokens != 350 {
		t.Fatalf("expected 350 total tokens, got %d", response.ProviderInfo.Usage.TotalTokens)
	}
}

func TestAIE2E_OpenRouterServerErrorFallback(t *testing.T) {
	var callCount int
	mockOR := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "Internal server error"},
		})
	}))
	defer mockOR.Close()

	cfg := config.Config{Addr: ":0", Env: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	eventStore := events.NewStore(100, events.DedupOptions{})

	pipeCfg := ai.PipelineConfig{
		AIMaxPromptChars:         40000,
		AIMaxCompletionTokens:    2000,
		AIMaxEventsPerRequest:    50,
		AIRetryMaxAttempts:       2,
		AIRetryBaseBackoffMs:     5,
		AIRetryMaxBackoffMs:      10,
		AIRatePerSecond:          1000,
		AIRateBurst:              100,
		AICostPerPromptToken:     0.00015,
		AICostPerCompletionToken: 0.00060,
		AIModel:                  "gpt-4o-mini",
		AIProvider:               "openrouter",
		OpenRouterAPIKey:         "sk-e2e-test-key",
		OpenRouterBaseURL:        mockOR.URL,
	}
	aiSvc := ai.ComposePipeline(pipeCfg, logger)

	handler := testAIHandler(t, cfg, frameStore, catalog, eventStore, aiSvc)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test-session/analyze", strings.NewReader(validAnalyzePayload))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 (fallback), got %d: %s", recorder.Code, recorder.Body.String())
	}
	if callCount != 2 {
		t.Fatalf("expected 2 calls (1 initial + 1 retry), got %d", callCount)
	}

	var response ai.GatewayResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Status != ai.StatusProviderError {
		t.Fatalf("expected provider_error, got %q", response.Status)
	}
	if !strings.Contains(response.Summary, "AI analysis is currently unavailable") {
		t.Fatalf("expected fallback summary, got %q", response.Summary)
	}
	if response.Error == "" {
		t.Fatal("expected non-empty error field on fallback")
	}
	if !strings.Contains(response.Error, "retry attempts exhausted") {
		t.Fatalf("expected error to mention retry exhaustion, got %q", response.Error)
	}
	if len(response.ReferencedEvents) == 0 {
		t.Fatal("expected referenced events on fallback")
	}
}

func TestAIE2E_OpenRouterRateLimitFallback(t *testing.T) {
	var callCount int
	mockOR := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "Rate limited"},
		})
	}))
	defer mockOR.Close()

	cfg := config.Config{Addr: ":0", Env: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	eventStore := events.NewStore(100, events.DedupOptions{})

	pipeCfg := ai.PipelineConfig{
		AIMaxPromptChars:         40000,
		AIMaxCompletionTokens:    2000,
		AIMaxEventsPerRequest:    50,
		AIRetryMaxAttempts:       2,
		AIRetryBaseBackoffMs:     5,
		AIRetryMaxBackoffMs:      10,
		AIRatePerSecond:          1000,
		AIRateBurst:              100,
		AICostPerPromptToken:     0.00015,
		AICostPerCompletionToken: 0.00060,
		AIModel:                  "gpt-4o-mini",
		AIProvider:               "openrouter",
		OpenRouterAPIKey:         "sk-e2e-test-key",
		OpenRouterBaseURL:        mockOR.URL,
	}
	aiSvc := ai.ComposePipeline(pipeCfg, logger)

	handler := testAIHandler(t, cfg, frameStore, catalog, eventStore, aiSvc)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test-session/analyze", strings.NewReader(validAnalyzePayload))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 (fallback), got %d: %s", recorder.Code, recorder.Body.String())
	}
	if callCount != 2 {
		t.Fatalf("expected 2 calls (1 initial + 1 retry), got %d", callCount)
	}

	var response ai.GatewayResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Status != ai.StatusProviderError {
		t.Fatalf("expected provider_error, got %q", response.Status)
	}
	if !strings.Contains(response.Summary, "AI analysis is currently unavailable") {
		t.Fatalf("expected fallback summary, got %q", response.Summary)
	}
	if response.Error == "" {
		t.Fatal("expected non-empty error field on fallback")
	}
	if !strings.Contains(response.Error, "retry attempts exhausted") {
		t.Fatalf("expected error to mention retry exhaustion, got %q", response.Error)
	}
	if len(response.ReferencedEvents) == 0 {
		t.Fatal("expected referenced events on fallback")
	}
}

func TestAIE2E_OpenRouterAuthRejectedFallback(t *testing.T) {
	var callCount int
	mockOR := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "Invalid API key"},
		})
	}))
	defer mockOR.Close()

	cfg := config.Config{Addr: ":0", Env: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	eventStore := events.NewStore(100, events.DedupOptions{})

	pipeCfg := ai.PipelineConfig{
		AIMaxPromptChars:         40000,
		AIMaxCompletionTokens:    2000,
		AIMaxEventsPerRequest:    50,
		AIRetryMaxAttempts:       2,
		AIRetryBaseBackoffMs:     5,
		AIRetryMaxBackoffMs:      10,
		AIRatePerSecond:          1000,
		AIRateBurst:              100,
		AICostPerPromptToken:     0.00015,
		AICostPerCompletionToken: 0.00060,
		AIModel:                  "gpt-4o-mini",
		AIProvider:               "openrouter",
		OpenRouterAPIKey:         "sk-bad-key",
		OpenRouterBaseURL:        mockOR.URL,
	}
	aiSvc := ai.ComposePipeline(pipeCfg, logger)

	handler := testAIHandler(t, cfg, frameStore, catalog, eventStore, aiSvc)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test-session/analyze", strings.NewReader(validAnalyzePayload))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 (fallback), got %d: %s", recorder.Code, recorder.Body.String())
	}
	if callCount != 1 {
		t.Fatalf("expected 1 call (401 is non-retryable), got %d", callCount)
	}

	var response ai.GatewayResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Status != ai.StatusProviderError {
		t.Fatalf("expected provider_error, got %q", response.Status)
	}
	if !strings.Contains(response.Summary, "AI analysis is currently unavailable") {
		t.Fatalf("expected fallback summary, got %q", response.Summary)
	}
	if response.Error == "" {
		t.Fatal("expected non-empty error field on fallback")
	}
	if len(response.ReferencedEvents) == 0 {
		t.Fatal("expected referenced events on fallback")
	}
}

func TestAIE2E_OpenRouterResponseNoRawTelemetryLeak(t *testing.T) {
	var capturedBody []byte
	mockOR := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-e2e-no-leak",
			"model": "gpt-4o-mini",
			"choices": []map[string]any{
				{
					"message":       map[string]any{"content": "clean analysis"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30,
			},
		})
	}))
	defer mockOR.Close()

	cfg := config.Config{Addr: ":0", Env: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	eventStore := events.NewStore(100, events.DedupOptions{})

	pipeCfg := ai.PipelineConfig{
		AIMaxPromptChars:         40000,
		AIMaxCompletionTokens:    2000,
		AIMaxEventsPerRequest:    50,
		AIRetryMaxAttempts:       2,
		AIRetryBaseBackoffMs:     5,
		AIRetryMaxBackoffMs:      10,
		AIRatePerSecond:          1000,
		AIRateBurst:              100,
		AICostPerPromptToken:     0.00015,
		AICostPerCompletionToken: 0.00060,
		AIModel:                  "gpt-4o-mini",
		AIProvider:               "openrouter",
		OpenRouterAPIKey:         "sk-e2e-test-key",
		OpenRouterBaseURL:        mockOR.URL,
	}
	aiSvc := ai.ComposePipeline(pipeCfg, logger)

	handler := testAIHandler(t, cfg, frameStore, catalog, eventStore, aiSvc)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test-session/analyze", strings.NewReader(validAnalyzePayload))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	bodyStr := string(capturedBody)
	for _, forbidden := range []string{`"positionX":`, `"positionY":`, `"positionZ":`, `"speedMps":`, `"rpm":`, `"gear":`, `"throttle":`, `"brake":`, `"steering":`, `"fuelLiters":`, `"wheelSpeedFL":`, `"wheelSpeedFR":`, `"wheelSpeedRL":`, `"wheelSpeedRR":`, `"isOnTrack":`, `"frames":`, `"telemetryFrames":`} {
		if strings.Contains(bodyStr, forbidden) {
			t.Fatalf("outgoing request must not contain raw telemetry field %q", forbidden)
		}
	}

	var response ai.GatewayResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	respBody := recorder.Body.String()
	for _, forbidden := range []string{`"positionX":`, `"positionY":`, `"positionZ":`, `"speedMps":`, `"rpm":`, `"gear":`, `"throttle":`, `"brake":`, `"steering":`, `"fuelLiters":`, `"wheelSpeedFL":`, `"wheelSpeedFR":`, `"wheelSpeedRL":`, `"wheelSpeedRR":`, `"isOnTrack":`, `"frames":`, `"telemetryFrames":`} {
		if strings.Contains(respBody, forbidden) {
			t.Fatalf("response must not contain raw telemetry field %q", forbidden)
		}
	}
}
