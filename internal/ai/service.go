package ai

import (
	"context"
)

const (
	DefaultAIModel     = "gpt-4o-mini"
	DefaultTemperature = 0.7
)

type Service struct {
	adapter     ProviderAdapter
	builder     PromptBuilder
	controller  *Controller
	model       string
	temperature float64
}

func NewService(adapter ProviderAdapter, controller *Controller) *Service {
	return &Service{
		adapter:     adapter,
		builder:     NewPromptBuilder(),
		controller:  controller,
		model:       DefaultAIModel,
		temperature: DefaultTemperature,
	}
}

func (s *Service) WithModel(model string) *Service {
	if model != "" {
		s.model = model
	}
	return s
}

func (s *Service) WithTemperature(temperature float64) *Service {
	if temperature > 0 {
		s.temperature = temperature
	}
	return s
}

func (s *Service) Analyze(ctx context.Context, req GatewayRequest) (GatewayResponse, error) {
	if err := req.Validate(); err != nil {
		return GatewayResponse{}, err
	}

	prompts := s.builder.Build(req)

	promptCharCount := len(prompts.SystemPrompt) + len(prompts.UserPrompt)
	providerReq := ProviderRequest{
		Model: s.model,
		Messages: []Message{
			{Role: "system", Content: prompts.SystemPrompt},
			{Role: "user", Content: prompts.UserPrompt},
		},
		Temperature: s.temperature,
		MaxTokens:   s.controller.Budget.MaxCompletionTokens,
	}

	resp, err := s.controller.Execute(ctx, req.Input.Session.SessionID, req.Mode, promptCharCount, len(req.Input.Events), providerReq, s.adapter)
	if err != nil {
		return GatewayResponse{}, err
	}

	return GatewayResponse{
		Summary:          resp.Content,
		ReferencedEvents: extractEventIDs(req.Input.Events),
		ProviderInfo: ProviderResultInfo{
			Model:        resp.Model,
			FinishReason: resp.FinishReason,
			Usage:        resp.Usage,
		},
	}, nil
}

var _ AIService = (*Service)(nil)
