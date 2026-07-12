package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUUIDProviderGeneratesUniqueIDs(t *testing.T) {
	p := UUIDProvider{}
	id1 := p.NewID()
	id2 := p.NewID()

	if id1 == "" || id2 == "" {
		t.Fatal("expected non-empty UUIDs")
	}
	if id1 == id2 {
		t.Fatal("expected consecutive UUIDs to differ")
	}
	if !strings.Contains(id1, "-") {
		t.Fatalf("expected UUID format with dashes, got %q", id1)
	}
}

func TestSequentialProviderProducesDeterministicIDs(t *testing.T) {
	p := &SequentialProvider{}
	id1 := p.NewID()
	id2 := p.NewID()

	if id1 != "trace-1" {
		t.Fatalf("expected trace-1, got %s", id1)
	}
	if id2 != "trace-2" {
		t.Fatalf("expected trace-2, got %s", id2)
	}
}

func TestSequentialProviderIsThreadSafe(t *testing.T) {
	p := &SequentialProvider{}
	var wg sync.WaitGroup
	ids := make(chan string, 100)

	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids <- p.NewID()
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[string]bool)
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		seen[id] = true
	}
	if len(seen) != 100 {
		t.Fatalf("expected 100 unique IDs, got %d", len(seen))
	}
}

func TestMemoryAuditStoreRecordAndRetrieve(t *testing.T) {
	store := NewMemoryAuditStore()
	now := time.Now()
	record := AuditRecord{
		TraceID:               "trace-1",
		SessionID:             "session-1",
		Mode:                  GatewayModeEngineer,
		SourceEventIDs:        []string{"event-1", "event-2"},
		ProviderModel:         "test-model",
		PromptTemplateVersion: PromptTemplateVersion,
		ConstraintSummary:     "2 constraints",
		ResponseSummary:       "test analysis",
		PromptTokenCount:      100,
		CompletionTokenCount:  50,
		TotalTokenCount:       150,
		DurationMs:            100,
		Status:                "success",
		RedactionVerified:     true,
		CreatedAt:             now,
	}

	err := store.Record(context.Background(), record)
	if err != nil {
		t.Fatalf("expected Record to succeed: %v", err)
	}
	if store.Len() != 1 {
		t.Fatalf("expected 1 record, got %d", store.Len())
	}

	retrieved, ok := store.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected to find trace-1")
	}
	if retrieved.SessionID != "session-1" {
		t.Fatalf("expected session-1, got %s", retrieved.SessionID)
	}
	if len(retrieved.SourceEventIDs) != 2 || retrieved.SourceEventIDs[0] != "event-1" {
		t.Fatalf("unexpected source event IDs: %v", retrieved.SourceEventIDs)
	}
}

func TestMemoryAuditStoreGetByTraceIDNotFound(t *testing.T) {
	store := NewMemoryAuditStore()
	_, ok := store.GetByTraceID("nonexistent")
	if ok {
		t.Fatal("expected false for nonexistent trace ID")
	}
}

func TestMemoryAuditStoreListBySession(t *testing.T) {
	store := NewMemoryAuditStore()
	ctx := context.Background()

	store.Record(ctx, AuditRecord{TraceID: "t1", SessionID: "session-a"})
	store.Record(ctx, AuditRecord{TraceID: "t2", SessionID: "session-b"})
	store.Record(ctx, AuditRecord{TraceID: "t3", SessionID: "session-a"})

	records := store.ListBySession("session-a")
	if len(records) != 2 {
		t.Fatalf("expected 2 records for session-a, got %d", len(records))
	}
	if records[0].TraceID != "t1" || records[1].TraceID != "t3" {
		t.Fatalf("unexpected records order: %v", records)
	}
}

func TestMemoryAuditStoreIsThreadSafe(t *testing.T) {
	store := NewMemoryAuditStore()
	ctx := context.Background()
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			record := AuditRecord{
				TraceID:   string(rune('a' + n)),
				SessionID: "shared-session",
			}
			_ = store.Record(ctx, record)
		}(i)
	}
	wg.Wait()

	if store.Len() != 100 {
		t.Fatalf("expected 100 records after concurrent writes, got %d", store.Len())
	}
}

func TestNewAuditedServiceDefaultIDProvider(t *testing.T) {
	inner := &fakeAIService{}
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(inner, logger, nil)

	if svc.idProvider == nil {
		t.Fatal("expected default ID provider when nil is passed")
	}

	id := svc.idProvider.NewID()
	if id == "" {
		t.Fatal("expected non-empty ID from default provider")
	}
}

func TestAuditedServiceRecordsSourceEventIDs(t *testing.T) {
	logger := NewMemoryAuditStore()
	p := &SequentialProvider{}
	svc := NewAuditedService(&fakeAIService{}, logger, p)

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record with trace-1")
	}

	if len(record.SourceEventIDs) != 1 || record.SourceEventIDs[0] != "event-1" {
		t.Fatalf("expected source event ID 'event-1', got %v", record.SourceEventIDs)
	}
}

func TestAuditedServiceRecordsSessionAndMode(t *testing.T) {
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(&fakeAIService{}, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeCoach)
	_, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if record.SessionID != "session-1" {
		t.Fatalf("expected session-1, got %s", record.SessionID)
	}
	if record.Mode != GatewayModeCoach {
		t.Fatalf("expected coach mode, got %s", record.Mode)
	}
}

func TestAuditedServiceRecordsUsageAndTiming(t *testing.T) {
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(&fakeAIService{}, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}

	if record.PromptTokenCount <= 0 {
		t.Fatalf("expected positive prompt token count, got %d", record.PromptTokenCount)
	}
	if record.CompletionTokenCount <= 0 {
		t.Fatalf("expected positive completion token count, got %d", record.CompletionTokenCount)
	}
	if record.TotalTokenCount <= 0 {
		t.Fatalf("expected positive total token count, got %d", record.TotalTokenCount)
	}
	if record.DurationMs < 0 {
		t.Fatalf("expected non-negative duration, got %d", record.DurationMs)
	}
}

func TestAuditedServiceRecordsProviderModel(t *testing.T) {
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(&fakeAIService{}, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if record.ProviderModel != "test-model" {
		t.Fatalf("expected test-model, got %s", record.ProviderModel)
	}
}

func TestAuditedServiceSuccessStatus(t *testing.T) {
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(&fakeAIService{}, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if record.Status != "success" {
		t.Fatalf("expected success status, got %s", record.Status)
	}
	if record.ErrorMessage != "" {
		t.Fatalf("expected empty error message on success, got %s", record.ErrorMessage)
	}
}

func TestAuditedServiceErrorStatus(t *testing.T) {
	logger := NewMemoryAuditStore()
	inner := &failingAIService{err: ErrProviderRejected}
	svc := NewAuditedService(inner, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from failing service")
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record even on error")
	}
	if record.Status != "error" {
		t.Fatalf("expected error status, got %s", record.Status)
	}
	if record.ErrorMessage == "" {
		t.Fatal("expected non-empty error message on error")
	}
	if !strings.Contains(record.ErrorMessage, "rejected") {
		t.Fatalf("expected error message to mention rejection, got %s", record.ErrorMessage)
	}
}

func TestAuditedServiceRecordsErrorForDifferentErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantMsg  string
	}{
		{"provider rejected", ErrProviderRejected, "rejected"},
		{"provider timeout", ErrProviderTimeout, "timed out"},
		{"provider not available", ErrProviderNotAvailable, "not available"},
		{"budget exceeded", ErrBudgetExceeded, "budget"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewMemoryAuditStore()
			inner := &failingAIService{err: tt.err}
			svc := NewAuditedService(inner, logger, &SequentialProvider{})

			req := validGatewayRequest(t, GatewayModeEngineer)
			svc.Analyze(context.Background(), req)

			record, ok := logger.GetByTraceID("trace-1")
			if !ok {
				t.Fatal("expected audit record")
			}
			if record.Status != "error" {
				t.Fatalf("expected error status, got %s", record.Status)
			}
			if !strings.Contains(record.ErrorMessage, tt.wantMsg) {
				t.Fatalf("expected error to contain %q, got %s", tt.wantMsg, record.ErrorMessage)
			}
		})
	}
}

func TestAuditedServiceRedactionVerifiedTrue(t *testing.T) {
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(&fakeAIService{}, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if !record.RedactionVerified {
		t.Fatal("expected RedactionVerified to be true")
	}
}

func TestAuditedServicePromptTemplateVersionSet(t *testing.T) {
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(&fakeAIService{}, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if record.PromptTemplateVersion != PromptTemplateVersion {
		t.Fatalf("expected %s, got %s", PromptTemplateVersion, record.PromptTemplateVersion)
	}
}

func TestAuditedServiceResponseSummaryTruncated(t *testing.T) {
	longSummary := strings.Repeat("x", 1000)

	inner := &fakeAIServiceWithSummary{summary: longSummary}
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(inner, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if len(record.ResponseSummary) > 500 {
		t.Fatalf("expected response summary <= 500 chars, got %d", len(record.ResponseSummary))
	}
	if !record.ResponseSummaryLimited {
		t.Fatal("expected ResponseSummaryLimited to be true for long summary")
	}
}

func TestAuditedServiceShortResponseSummaryNotTruncated(t *testing.T) {
	inner := &fakeAIServiceWithSummary{summary: "short summary"}
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(inner, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if record.ResponseSummary != "short summary" {
		t.Fatalf("expected full summary, got %s", record.ResponseSummary)
	}
	if record.ResponseSummaryLimited {
		t.Fatal("expected ResponseSummaryLimited to be false for short summary")
	}
}

func TestAuditedServiceConstraintSummaryFormat(t *testing.T) {
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(&fakeAIService{}, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if !strings.Contains(record.ConstraintSummary, "constraints") {
		t.Fatalf("expected constraint summary to mention constraints, got %s", record.ConstraintSummary)
	}
}

func TestAuditedServiceConstraintSummaryDoesNotLeakFullContent(t *testing.T) {
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(&fakeAIService{}, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.Constraints = []string{
		"AI receives structured Engineer events and derived metric evidence only",
		"do not invent track names",
	}

	_, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if strings.Contains(record.ConstraintSummary, "invent") {
		t.Fatalf("constraint summary should not leak full constraint content, got %q", record.ConstraintSummary)
	}
}

func TestAuditedServiceAuditFailureDoesNotFailRequest(t *testing.T) {
	recorded := false
	logger := &failingAuditLogger{}
	svc := NewAuditedService(&fakeAIService{}, logger, &SequentialProvider{})

	req := validGatewayRequest(t, GatewayModeEngineer)
	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected request to succeed despite audit failure: %v", err)
	}
	if resp.Summary == "" {
		t.Fatal("expected non-empty response summary")
	}
	if recorded {
		t.Fatal("expected no records in failing logger")
	}
}

func TestAuditedServiceCreatedAtIsRecent(t *testing.T) {
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(&fakeAIService{}, logger, &SequentialProvider{})

	before := time.Now()
	req := validGatewayRequest(t, GatewayModeEngineer)
	_, err := svc.Analyze(context.Background(), req)
	after := time.Now()
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}

	record, ok := logger.GetByTraceID("trace-1")
	if !ok {
		t.Fatal("expected audit record")
	}
	if record.CreatedAt.Before(before) || record.CreatedAt.After(after) {
		t.Fatalf("expected CreatedAt between %v and %v, got %v", before, after, record.CreatedAt)
	}
}

func TestAuditedServiceSatisfiesAIService(t *testing.T) {
	var svc AIService = NewAuditedService(&fakeAIService{}, NewMemoryAuditStore(), nil)
	req := validGatewayRequest(t, GatewayModeEngineer)
	resp, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if resp.Summary == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestAuditedServiceRecordsMultipleRequests(t *testing.T) {
	logger := NewMemoryAuditStore()
	svc := NewAuditedService(&fakeAIService{}, logger, &SequentialProvider{})

	req1 := validGatewayRequest(t, GatewayModeEngineer)
	req2 := validGatewayRequest(t, GatewayModeCoach)

	_, err := svc.Analyze(context.Background(), req1)
	if err != nil {
		t.Fatalf("expected first success: %v", err)
	}

	_, err = svc.Analyze(context.Background(), req2)
	if err != nil {
		t.Fatalf("expected second success: %v", err)
	}

	if logger.Len() != 2 {
		t.Fatalf("expected 2 audit records, got %d", logger.Len())
	}

	r1, _ := logger.GetByTraceID("trace-1")
	r2, _ := logger.GetByTraceID("trace-2")

	if r1.Mode != GatewayModeEngineer {
		t.Fatalf("expected first record engineer mode, got %s", r1.Mode)
	}
	if r2.Mode != GatewayModeCoach {
		t.Fatalf("expected second record coach mode, got %s", r2.Mode)
	}
}

func TestBuildConstraintSummaryEmpty(t *testing.T) {
	s := buildConstraintSummary(nil)
	if s != "no constraints" {
		t.Fatalf("expected 'no constraints', got %q", s)
	}
	s = buildConstraintSummary([]string{})
	if s != "no constraints" {
		t.Fatalf("expected 'no constraints' for empty slice, got %q", s)
	}
}

func TestBuildConstraintSummaryWithCount(t *testing.T) {
	s := buildConstraintSummary([]string{"a", "b", "c"})
	if s != "3 constraints" {
		t.Fatalf("expected '3 constraints', got %q", s)
	}
}

func TestExtractEventIDs(t *testing.T) {
	input := validConsumerInput()
	ids := extractEventIDs(input.Events)
	if len(ids) != 1 || ids[0] != "event-1" {
		t.Fatalf("unexpected IDs: %v", ids)
	}

	second := input.Events[0]
	second.Event.EventID = "event-2"
	input.Events = append(input.Events, second)
	ids = extractEventIDs(input.Events)
	if len(ids) != 2 || ids[0] != "event-1" || ids[1] != "event-2" {
		t.Fatalf("unexpected IDs for two events: %v", ids)
	}
}

func TestExtractEventIDsEmpty(t *testing.T) {
	ids := extractEventIDs(nil)
	if len(ids) != 0 {
		t.Fatalf("expected empty slice for nil, got %d", len(ids))
	}
	ids = extractEventIDs([]EventEnvelope{})
	if len(ids) != 0 {
		t.Fatalf("expected empty slice, got %d", len(ids))
	}
}

type failingAIService struct {
	err error
}

func (f *failingAIService) Analyze(_ context.Context, _ GatewayRequest) (GatewayResponse, error) {
	return GatewayResponse{}, f.err
}

type fakeAIServiceWithSummary struct {
	summary string
}

func (f *fakeAIServiceWithSummary) Analyze(_ context.Context, req GatewayRequest) (GatewayResponse, error) {
	explanations := make([]EventExplanation, len(req.Input.Events))
	for i, env := range req.Input.Events {
		explanations[i] = EventExplanation{
			EventID:     env.Event.EventID,
			Type:        env.Event.Type,
			Explanation: "AI analysis for event " + env.Event.EventID,
			Relevance:   0.75,
		}
	}
	return GatewayResponse{
		Summary:           f.summary,
		EventExplanations: explanations,
		Recommendations:   []string{"work on consistency"},
		ReferencedEvents:  extractEventIDs(req.Input.Events),
		ProviderInfo: ProviderResultInfo{
			Model:        "test-model",
			FinishReason: "stop",
			Usage:        UsageInfo{PromptTokens: 50, CompletionTokens: 100, TotalTokens: 150},
		},
	}, nil
}

type failingAuditLogger struct{}

func (f *failingAuditLogger) Record(_ context.Context, _ AuditRecord) error {
	return errors.New("audit storage unavailable")
}


