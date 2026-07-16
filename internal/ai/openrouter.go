package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"
	openRouterChatPath       = "/chat/completions"
	openRouterRequestTimeout = 30 * time.Second
)

var maxRetryAfterSeconds = int(^uint(0) >> 1)

type OpenRouterConfig struct {
	APIKey      string
	BaseURL     string
	HTTPReferer string
	Title       string
}

type OpenRouterAdapter struct {
	config OpenRouterConfig
	client *http.Client
}

type OpenRouterOption func(*OpenRouterAdapter)

func WithOpenRouterHTTPClient(client *http.Client) OpenRouterOption {
	return func(a *OpenRouterAdapter) {
		a.client = client
	}
}

func NewOpenRouterAdapter(config OpenRouterConfig, opts ...OpenRouterOption) *OpenRouterAdapter {
	if config.BaseURL == "" {
		config.BaseURL = DefaultOpenRouterBaseURL
	}
	a := &OpenRouterAdapter{
		config: config,
		client: &http.Client{Timeout: openRouterRequestTimeout},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (a *OpenRouterAdapter) Analyze(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	openReq := openRouterRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	for _, msg := range req.Messages {
		openReq.Messages = append(openReq.Messages, openRouterMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	body, err := json.Marshal(openReq)
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("%w: marshal request: %v", ErrProviderNotAvailable, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.chatEndpoint(), bytes.NewReader(body))
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("%w: create request: %v", ErrProviderNotAvailable, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	if a.config.HTTPReferer != "" {
		httpReq.Header.Set("HTTP-Referer", a.config.HTTPReferer)
	}
	if a.config.Title != "" {
		httpReq.Header.Set("X-OpenRouter-Title", a.config.Title)
	}

	httpResp, err := a.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return ProviderResponse{}, ErrProviderTimeout
		}
		return ProviderResponse{}, fmt.Errorf("%w: request failed: %v", ErrProviderNotAvailable, err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("%w: read response: %v", ErrProviderNotAvailable, err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return ProviderResponse{}, a.mapHTTPError(httpResp.StatusCode, httpResp.Header, respBody, req.Model)
	}

	var openResp openRouterResponse
	if err := json.Unmarshal(respBody, &openResp); err != nil {
		return ProviderResponse{}, fmt.Errorf("%w: parse response: %v", ErrProviderNotAvailable, err)
	}

	if len(openResp.Choices) == 0 {
		return ProviderResponse{}, ErrInvalidProviderResponse
	}

	content := openResp.Choices[0].Message.Content
	finishReason := openResp.Choices[0].FinishReason

	usage := UsageInfo{}
	if openResp.Usage != nil {
		usage.PromptTokens = openResp.Usage.PromptTokens
		usage.CompletionTokens = openResp.Usage.CompletionTokens
		usage.TotalTokens = openResp.Usage.TotalTokens
	}

	return ProviderResponse{
		Content:      content,
		Model:        openResp.Model,
		FinishReason: finishReason,
		Usage:        usage,
	}, nil
}

func (a *OpenRouterAdapter) chatEndpoint() string {
	base := strings.TrimRight(a.config.BaseURL, "/")
	return base + openRouterChatPath
}

func (a *OpenRouterAdapter) mapHTTPError(statusCode int, headers http.Header, body []byte, model string) error {
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return ErrProviderRejected
	case statusCode == http.StatusBadRequest:
		return ErrProviderRejected
	case statusCode == http.StatusTooManyRequests:
		return a.parseRateLimitError(headers, body, model)
	case statusCode >= 500:
		return ErrProviderNotAvailable
	case statusCode < 200 || statusCode >= 300:
		return fmt.Errorf("%w: unexpected HTTP %d", ErrProviderNotAvailable, statusCode)
	default:
		return nil
	}
}

func (a *OpenRouterAdapter) parseRateLimitError(headers http.Header, body []byte, model string) error {
	rateLimitErr := &ProviderRateLimitError{
		Err:   ErrProviderNotAvailable,
		Model: model,
	}
	if retryAfter, ok := parseRetryAfterHeader(headers.Get("Retry-After")); ok {
		rateLimitErr.RetryAfterSeconds = &retryAfter
	}

	var errBody openRouterErrorBody
	if err := json.Unmarshal(body, &errBody); err == nil {
		if rateLimitErr.RetryAfterSeconds == nil {
			if retryAfter, ok := errBody.Error.Metadata.retryAfterSeconds(); ok {
				rateLimitErr.RetryAfterSeconds = &retryAfter
			}
		}
		if errBody.Error.Metadata.ProviderName != "" {
			rateLimitErr.ProviderName = errBody.Error.Metadata.ProviderName
		}
	}

	return rateLimitErr
}

func parseRetryAfterHeader(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	seconds, err := strconv.Atoi(value)
	if err == nil && seconds > 0 {
		return seconds, true
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		if seconds, ok := positiveCeilSeconds(time.Until(retryAt).Seconds()); ok {
			return seconds, true
		}
	}
	return 0, false
}

type openRouterErrorBody struct {
	Error openRouterErrorDetail `json:"error"`
}

type openRouterErrorDetail struct {
	Code     int                     `json:"code"`
	Message  string                  `json:"message"`
	Metadata openRouterErrorMetadata `json:"metadata"`
}

type openRouterErrorMetadata struct {
	RetryAfterSeconds    json.RawMessage `json:"retry_after_seconds"`
	RetryAfterSecondsRaw json.RawMessage `json:"retry_after_seconds_raw"`
	ProviderName         string          `json:"provider_name"`
}

func (m openRouterErrorMetadata) retryAfterSeconds() (int, bool) {
	if seconds, ok := parsePositiveSeconds(m.RetryAfterSeconds); ok {
		return seconds, true
	}
	return parsePositiveSeconds(m.RetryAfterSecondsRaw)
}

func parsePositiveSeconds(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return positiveCeilSeconds(n)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err == nil {
			return positiveCeilSeconds(parsed)
		}
	}
	return 0, false
}

func positiveCeilSeconds(seconds float64) (int, bool) {
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > float64(maxRetryAfterSeconds) {
		return 0, false
	}
	return int(math.Ceil(seconds)), true
}

type openRouterRequest struct {
	Model       string              `json:"model"`
	Messages    []openRouterMessage `json:"messages"`
	Temperature float64             `json:"temperature,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterResponse struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Choices []openRouterChoice `json:"choices"`
	Usage   *openRouterUsage   `json:"usage,omitempty"`
}

type openRouterChoice struct {
	Message      openRouterResponseMessage `json:"message"`
	FinishReason string                    `json:"finish_reason"`
}

type openRouterResponseMessage struct {
	Content string `json:"content"`
}

type openRouterUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

var _ ProviderAdapter = (*OpenRouterAdapter)(nil)
