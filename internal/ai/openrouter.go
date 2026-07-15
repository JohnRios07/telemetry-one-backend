package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"
	openRouterChatPath       = "/chat/completions"
	openRouterRequestTimeout = 30 * time.Second
)

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
		return ProviderResponse{}, a.mapHTTPError(httpResp.StatusCode, respBody, req.Model)
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

func (a *OpenRouterAdapter) mapHTTPError(statusCode int, body []byte, model string) error {
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return ErrProviderRejected
	case statusCode == http.StatusBadRequest:
		return ErrProviderRejected
	case statusCode == http.StatusTooManyRequests:
		return a.parseRateLimitError(body, model)
	case statusCode >= 500:
		return ErrProviderNotAvailable
	case statusCode < 200 || statusCode >= 300:
		return fmt.Errorf("%w: unexpected HTTP %d", ErrProviderNotAvailable, statusCode)
	default:
		return nil
	}
}

func (a *OpenRouterAdapter) parseRateLimitError(body []byte, model string) error {
	rateLimitErr := &ProviderRateLimitError{
		Err:   ErrProviderNotAvailable,
		Model: model,
	}

	var errBody openRouterErrorBody
	if err := json.Unmarshal(body, &errBody); err == nil {
		if errBody.Error.Metadata.RetryAfterSeconds > 0 {
			rateLimitErr.RetryAfterSeconds = &errBody.Error.Metadata.RetryAfterSeconds
		}
		if errBody.Error.Metadata.ProviderName != "" {
			rateLimitErr.ProviderName = errBody.Error.Metadata.ProviderName
		}
	}

	return rateLimitErr
}

type openRouterErrorBody struct {
	Error openRouterErrorDetail `json:"error"`
}

type openRouterErrorDetail struct {
	Code     int                      `json:"code"`
	Message  string                   `json:"message"`
	Metadata openRouterErrorMetadata `json:"metadata"`
}

type openRouterErrorMetadata struct {
	RetryAfterSeconds int    `json:"retry_after_seconds"`
	ProviderName      string `json:"provider_name"`
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
