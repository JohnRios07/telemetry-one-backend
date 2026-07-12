package ai

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

type IDProvider interface {
	NewID() string
}

type UUIDProvider struct{}

func (UUIDProvider) NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type SequentialProvider struct {
	mu sync.Mutex
	n  int
}

func (p *SequentialProvider) NewID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n++
	return fmt.Sprintf("trace-%d", p.n)
}

type AuditRecord struct {
	TraceID                string    `json:"traceId"`
	SessionID              string    `json:"sessionId"`
	Mode                   string    `json:"mode"`
	SourceEventIDs         []string  `json:"sourceEventIDs"`
	ProviderModel          string    `json:"providerModel,omitempty"`
	PromptTemplateVersion  string    `json:"promptTemplateVersion"`
	ConstraintSummary      string    `json:"constraintSummary,omitempty"`
	ResponseSummary        string    `json:"responseSummary,omitempty"`
	ResponseSummaryLimited bool      `json:"responseSummaryLimited"`
	PromptTokenCount       int       `json:"promptTokenCount"`
	CompletionTokenCount   int       `json:"completionTokenCount"`
	TotalTokenCount        int       `json:"totalTokenCount"`
	DurationMs             int64     `json:"durationMs"`
	Status                 string    `json:"status"`
	ErrorMessage           string    `json:"error,omitempty"`
	RedactionVerified      bool      `json:"redactionVerified"`
	CreatedAt              time.Time `json:"createdAt"`
}

type AuditLogger interface {
	Record(ctx context.Context, record AuditRecord) error
}

type MemoryAuditStore struct {
	mu        sync.Mutex
	records   []AuditRecord
	byTraceID map[string]*AuditRecord
}

func NewMemoryAuditStore() *MemoryAuditStore {
	return &MemoryAuditStore{
		byTraceID: make(map[string]*AuditRecord),
	}
}

func (s *MemoryAuditStore) Record(_ context.Context, record AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	s.byTraceID[record.TraceID] = &s.records[len(s.records)-1]
	return nil
}

func (s *MemoryAuditStore) GetByTraceID(traceID string) (AuditRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byTraceID[traceID]
	if !ok {
		return AuditRecord{}, false
	}
	return *r, true
}

func (s *MemoryAuditStore) ListBySession(sessionID string) []AuditRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []AuditRecord
	for _, r := range s.records {
		if r.SessionID == sessionID {
			result = append(result, r)
		}
	}
	return result
}

func (s *MemoryAuditStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

type AuditedService struct {
	inner      AIService
	logger     AuditLogger
	idProvider IDProvider
}

func NewAuditedService(inner AIService, logger AuditLogger, idProvider IDProvider) *AuditedService {
	if idProvider == nil {
		idProvider = UUIDProvider{}
	}
	return &AuditedService{
		inner:      inner,
		logger:     logger,
		idProvider: idProvider,
	}
}

func (s *AuditedService) Analyze(ctx context.Context, req GatewayRequest) (GatewayResponse, error) {
	traceID := s.idProvider.NewID()
	start := time.Now()

	resp, err := s.inner.Analyze(ctx, req)
	duration := time.Since(start)

	sourceEventIDs := extractEventIDs(req.Input.Events)
	constraintSummary := buildConstraintSummary(req.Input.Constraints)

	var responseSummary string
	responseLimited := false
	var model string
	var promptTokens, completionTokens, totalTokens int
	if err == nil {
		responseSummary = resp.Summary
		if len(responseSummary) > 500 {
			responseSummary = responseSummary[:500]
			responseLimited = true
		}
		model = resp.ProviderInfo.Model
		promptTokens = resp.ProviderInfo.Usage.PromptTokens
		completionTokens = resp.ProviderInfo.Usage.CompletionTokens
		totalTokens = resp.ProviderInfo.Usage.TotalTokens
	}

	status := resp.Status
	if status == "" {
		status = "success"
	}
	var errorMsg string
	if err != nil {
		status = "error"
		errorMsg = err.Error()
	} else if resp.Error != "" {
		errorMsg = resp.Error
	}

	record := AuditRecord{
		TraceID:                traceID,
		SessionID:              req.Input.Session.SessionID,
		Mode:                   req.Mode,
		SourceEventIDs:         sourceEventIDs,
		ProviderModel:          model,
		PromptTemplateVersion:  PromptTemplateVersion,
		ConstraintSummary:      constraintSummary,
		ResponseSummary:        responseSummary,
		ResponseSummaryLimited: responseLimited,
		PromptTokenCount:       promptTokens,
		CompletionTokenCount:   completionTokens,
		TotalTokenCount:        totalTokens,
		DurationMs:             duration.Milliseconds(),
		Status:                 status,
		ErrorMessage:           errorMsg,
		RedactionVerified:      true,
		CreatedAt:              time.Now(),
	}

	if auditErr := s.logger.Record(ctx, record); auditErr != nil {
		_ = auditErr
	}

	return resp, err
}

func extractEventIDs(events []EventEnvelope) []string {
	ids := make([]string, len(events))
	for i, env := range events {
		ids[i] = env.Event.EventID
	}
	return ids
}

func buildConstraintSummary(constraints []string) string {
	if len(constraints) == 0 {
		return "no constraints"
	}
	return fmt.Sprintf("%d constraints", len(constraints))
}

var _ AIService = (*AuditedService)(nil)
