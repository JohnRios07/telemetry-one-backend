package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenRouterAdapterSendsAuthHeader(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		writeOpenRouterOK(w, "analysis result", "gpt-4o-mini", "stop", nil)
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test-key",
		BaseURL: server.URL,
	})

	_, err := adapter.Analyze(context.Background(), validProviderRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if authHeader != "Bearer sk-test-key" {
		t.Fatalf("expected Authorization: Bearer sk-test-key, got %q", authHeader)
	}
}

func TestOpenRouterAdapterSendsCorrectPayload(t *testing.T) {
	var got openRouterRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		writeOpenRouterOK(w, "analysis", "gpt-4o-mini", "stop", nil)
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})

	req := ProviderRequest{
		Model: "gpt-4o-mini",
		Messages: []Message{
			{Role: "system", Content: "You are an AI assistant"},
			{Role: "user", Content: "Analyze this session"},
		},
		Temperature: 0.5,
		MaxTokens:   1000,
	}

	_, err := adapter.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Model != "gpt-4o-mini" {
		t.Fatalf("expected model gpt-4o-mini, got %q", got.Model)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got.Messages))
	}
	if got.Messages[0].Role != "system" || got.Messages[0].Content != "You are an AI assistant" {
		t.Fatalf("unexpected first message: %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content != "Analyze this session" {
		t.Fatalf("unexpected second message: %+v", got.Messages[1])
	}
	if got.Temperature != 0.5 {
		t.Fatalf("expected temperature 0.5, got %f", got.Temperature)
	}
	if got.MaxTokens != 1000 {
		t.Fatalf("expected max_tokens 1000, got %d", got.MaxTokens)
	}
}

func TestOpenRouterAdapterSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOpenRouterOK(w, "The session shows improvement in corner entry speed.",
			"gpt-4o-mini", "stop", &openRouterUsage{
				PromptTokens: 120, CompletionTokens: 230, TotalTokens: 350,
			})
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})

	resp, err := adapter.Analyze(context.Background(), validProviderRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "The session shows improvement in corner entry speed." {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if resp.Model != "gpt-4o-mini" {
		t.Fatalf("expected model gpt-4o-mini, got %q", resp.Model)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("expected finish_reason stop, got %q", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 120 {
		t.Fatalf("expected 120 prompt tokens, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 230 {
		t.Fatalf("expected 230 completion tokens, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 350 {
		t.Fatalf("expected 350 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

func TestOpenRouterAdapterUsageIsZeroWhenNotProvided(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOpenRouterOK(w, "result", "gpt-4o-mini", "stop", nil)
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})

	resp, err := adapter.Analyze(context.Background(), validProviderRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Usage.PromptTokens != 0 || resp.Usage.CompletionTokens != 0 || resp.Usage.TotalTokens != 0 {
		t.Fatalf("expected zero usage when not provided, got %+v", resp.Usage)
	}
}

func TestOpenRouterAdapterContentTypeHeader(t *testing.T) {
	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		writeOpenRouterOK(w, "ok", "model", "stop", nil)
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})

	_, err := adapter.Analyze(context.Background(), validProviderRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contentType != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", contentType)
	}
}

func TestOpenRouterAdapterHTTPRefererHeader(t *testing.T) {
	var gotReferer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReferer = r.Header.Get("HTTP-Referer")
		writeOpenRouterOK(w, "ok", "model", "stop", nil)
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:      "sk-test",
		BaseURL:     server.URL,
		HTTPReferer: "https://telemetryone.app",
	})

	_, err := adapter.Analyze(context.Background(), validProviderRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReferer != "https://telemetryone.app" {
		t.Fatalf("expected HTTP-Referer https://telemetryone.app, got %q", gotReferer)
	}
}

func TestOpenRouterAdapterTitleHeader(t *testing.T) {
	var gotTitle string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle = r.Header.Get("X-OpenRouter-Title")
		writeOpenRouterOK(w, "ok", "model", "stop", nil)
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey: "sk-test",
		BaseURL: server.URL,
		Title:  "Telemetry One",
	})

	_, err := adapter.Analyze(context.Background(), validProviderRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTitle != "Telemetry One" {
		t.Fatalf("expected X-OpenRouter-Title Telemetry One, got %q", gotTitle)
	}
}

func TestOpenRouterAdapterOmitsOptionalHeadersWhenEmpty(t *testing.T) {
	var hasReferer, hasTitle bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hasReferer = r.Header.Get("HTTP-Referer") != ""
		hasTitle = r.Header.Get("X-OpenRouter-Title") != ""
		writeOpenRouterOK(w, "ok", "model", "stop", nil)
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})

	_, err := adapter.Analyze(context.Background(), validProviderRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasReferer {
		t.Fatal("expected no HTTP-Referer header when not configured")
	}
	if hasTitle {
		t.Fatal("expected no X-OpenRouter-Title header when not configured")
	}
}

func TestOpenRouterAdapterRejects401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"Invalid API key"}}`)
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-bad-key",
		BaseURL: server.URL,
	})

	_, err := adapter.Analyze(context.Background(), validProviderRequest())
	if !errors.Is(err, ErrProviderRejected) {
		t.Fatalf("expected ErrProviderRejected for 401, got %v", err)
	}
}

func TestOpenRouterAdapterRejects403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"message":"Forbidden"}}`)
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-forbidden",
		BaseURL: server.URL,
	})

	_, err := adapter.Analyze(context.Background(), validProviderRequest())
	if !errors.Is(err, ErrProviderRejected) {
		t.Fatalf("expected ErrProviderRejected for 403, got %v", err)
	}
}

func TestOpenRouterAdapterRejects400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"Bad request"}}`)
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})

	_, err := adapter.Analyze(context.Background(), validProviderRequest())
	if !errors.Is(err, ErrProviderRejected) {
		t.Fatalf("expected ErrProviderRejected for 400, got %v", err)
	}
}

func TestOpenRouterAdapterClassifies429AsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"Rate limited"}}`)
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})

	_, err := adapter.Analyze(context.Background(), validProviderRequest())
	if !errors.Is(err, ErrProviderNotAvailable) {
		t.Fatalf("expected ErrProviderNotAvailable (retryable) for 429, got %v", err)
	}
}

func TestOpenRouterAdapterClassifies5xxAsRetryable(t *testing.T) {
	for _, code := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer server.Close()

			adapter := NewOpenRouterAdapter(OpenRouterConfig{
				APIKey:  "sk-test",
				BaseURL: server.URL,
			})

			_, err := adapter.Analyze(context.Background(), validProviderRequest())
			if !errors.Is(err, ErrProviderNotAvailable) {
				t.Fatalf("expected ErrProviderNotAvailable (retryable) for %d, got %v", code, err)
			}
		})
	}
}

func TestOpenRouterAdapterMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `this is not valid json`)
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})

	_, err := adapter.Analyze(context.Background(), validProviderRequest())
	if err == nil {
		t.Fatal("expected error for malformed JSON response")
	}
}

func TestOpenRouterAdapterEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(openRouterResponse{
			ID:    "chatcmpl-123",
			Model: "gpt-4o-mini",
		})
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})

	_, err := adapter.Analyze(context.Background(), validProviderRequest())
	if !errors.Is(err, ErrInvalidProviderResponse) {
		t.Fatalf("expected ErrInvalidProviderResponse for empty choices, got %v", err)
	}
}

func TestOpenRouterAdapterDefaultBaseURL(t *testing.T) {
	adapter := NewOpenRouterAdapter(OpenRouterConfig{APIKey: "sk-test"})
	expected := "https://openrouter.ai/api/v1/api/v1/chat/completions"
	got := adapter.chatEndpoint()
	if got != expected {
		t.Fatalf("expected endpoint %q, got %q", expected, got)
	}
}

func TestOpenRouterAdapterCustomBaseURL(t *testing.T) {
	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: "https://custom.openrouter.ai",
	})
	expected := "https://custom.openrouter.ai/api/v1/chat/completions"
	got := adapter.chatEndpoint()
	if got != expected {
		t.Fatalf("expected endpoint %q, got %q", expected, got)
	}
}

func TestOpenRouterAdapterSatisfiesProviderAdapterInterface(t *testing.T) {
	var adapter ProviderAdapter = NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: "http://localhost:0",
	})

	_ = adapter
}

func TestOpenRouterAdapterContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: server.URL,
	}, WithOpenRouterHTTPClient(&http.Client{Timeout: 0}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adapter.Analyze(ctx, validProviderRequest())
	if !errors.Is(err, ErrProviderTimeout) {
		t.Fatalf("expected ErrProviderTimeout for canceled context, got %v", err)
	}
}

func TestOpenRouterAdapterNoLiveNetwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOpenRouterOK(w, "test", "model", "stop", &openRouterUsage{
			PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30,
		})
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(OpenRouterConfig{
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})

	resp, err := adapter.Analyze(context.Background(), validProviderRequest())
	if err != nil {
		t.Fatalf("expected success with httptest server (no live network): %v", err)
	}
	if resp.Content != "test" {
		t.Fatalf("expected content 'test', got %q", resp.Content)
	}
	if resp.Usage.TotalTokens != 30 {
		t.Fatalf("expected 30 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

func writeOpenRouterOK(w http.ResponseWriter, content, model, finishReason string, usage *openRouterUsage) {
	w.WriteHeader(http.StatusOK)
	resp := openRouterResponse{
		ID:    "chatcmpl-test",
		Model: model,
		Choices: []openRouterChoice{
			{
				Message:      openRouterResponseMessage{Content: content},
				FinishReason: finishReason,
			},
		},
	}
	if usage != nil {
		resp.Usage = usage
	}
	json.NewEncoder(w).Encode(resp)
}

func validProviderRequest() ProviderRequest {
	return ProviderRequest{
		Model: "gpt-4o-mini",
		Messages: []Message{
			{Role: "user", Content: "test input"},
		},
		Temperature: 0.7,
		MaxTokens:   500,
	}
}
