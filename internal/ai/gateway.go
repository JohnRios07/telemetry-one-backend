package ai

import (
	"context"
	"errors"

	"telemetry-one-backend/internal/events"
)

var (
	ErrEmptyGatewayRequest    = errors.New("gateway request must not be empty")
	ErrNoEventsInGateway      = errors.New("gateway request must contain at least one engineer event or derived signal")
	ErrUnsupportedGatewayMode = errors.New("unsupported gateway mode")
	ErrProviderNotAvailable   = errors.New("AI provider is not available")
	ErrProviderRejected       = errors.New("AI provider rejected the request")
	ErrProviderTimeout        = errors.New("AI provider request timed out")
)

const (
	GatewayModeEngineer = "engineer"
	GatewayModeCoach    = "coach"
)

type ProviderAdapter interface {
	Analyze(ctx context.Context, req ProviderRequest) (ProviderResponse, error)
}

type ProviderRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"maxTokens"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ProviderResponse struct {
	Content      string    `json:"content"`
	Model        string    `json:"model"`
	FinishReason string    `json:"finishReason"`
	Usage        UsageInfo `json:"usage"`
}

type UsageInfo struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

type AIService interface {
	Analyze(ctx context.Context, req GatewayRequest) (GatewayResponse, error)
}

type GatewayRequest struct {
	Input ConsumerInput `json:"input"`
	Mode  string        `json:"mode"`
}

func (r GatewayRequest) Validate() error {
	if r.Input.ContractVersion == "" {
		return ErrEmptyGatewayRequest
	}
	if len(r.Input.Events) == 0 && len(r.Input.Signals) == 0 {
		return ErrNoEventsInGateway
	}
	if err := r.Input.Validate(); err != nil {
		return err
	}
	switch r.Mode {
	case GatewayModeEngineer, GatewayModeCoach:
		return nil
	default:
		return ErrUnsupportedGatewayMode
	}
}

type GatewayResponse struct {
	Summary           string              `json:"summary"`
	EventExplanations []EventExplanation  `json:"eventExplanations,omitempty"`
	Recommendations   []string            `json:"recommendations,omitempty"`
	ReferencedEvents  []string            `json:"referencedEvents,omitempty"`
	Signals           []Signal            `json:"signals,omitempty"`
	ProviderInfo      ProviderResultInfo  `json:"providerInfo"`
	Status            string              `json:"status"`
	Error             string              `json:"error,omitempty"`
}

type EventExplanation struct {
	EventID     string         `json:"eventId"`
	Type        events.EventType `json:"type"`
	Explanation string         `json:"explanation"`
	Relevance   float64        `json:"relevance"`
}

type ProviderResultInfo struct {
	Model             string    `json:"model"`
	FinishReason      string    `json:"finishReason"`
	Usage             UsageInfo `json:"usage"`
	RetryAfterSeconds *int      `json:"retryAfterSeconds,omitempty"`
	ProviderName      string    `json:"providerName,omitempty"`
}
