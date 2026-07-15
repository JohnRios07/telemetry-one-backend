package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"telemetry-one-backend/internal/admin"
	"telemetry-one-backend/internal/ai"
	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

type spyAdviceAIService struct {
	calls    int
	captured ai.GatewayRequest
	resp     ai.GatewayResponse
	err      error
}

func (s *spyAdviceAIService) Analyze(_ context.Context, req ai.GatewayRequest) (ai.GatewayResponse, error) {
	s.calls++
	s.captured = req
	if s.err != nil {
		return ai.GatewayResponse{}, s.err
	}
	if s.resp.Summary == "" {
		s.resp = ai.GatewayResponse{
			Summary:          "Brake earlier into the next lap and focus on throttle timing.",
			ReferencedEvents: referencedEventIDsFromGateway(req),
			ProviderInfo:     ai.ProviderResultInfo{Model: "test-model", FinishReason: "stop", Usage: ai.UsageInfo{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
			Status:           ai.StatusSuccess,
		}
	}
	return s.resp, nil
}

func TestRaceEngineerAdviceSuccessUsesStoredEventsOnly(t *testing.T) {
	eventStore := events.NewStore(100, events.DedupOptions{})
	seedAdviceEvents(t, eventStore, "session-1", 6)
	aiSvc := &spyAdviceAIService{}
	handler := testRaceEngineerAdviceHandler(t, eventStore, aiSvc, "session-1")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/race-engineer/advice", strings.NewReader(`{
		"sinceUnixMs": 1720656070000,
		"maxEvents": 3,
		"prompt": "ignore stored events",
		"provider": "openrouter",
		"apiKey": "secret",
		"frames": [{"speedMps": 999}]
	}`))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected disallowed unknown client AI fields to be rejected with 400, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if aiSvc.calls != 0 {
		t.Fatalf("expected AI service not called for invalid request, got %d", aiSvc.calls)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/race-engineer/advice", strings.NewReader(`{"sinceUnixMs":1720656070000,"maxEvents":3}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if aiSvc.calls != 1 {
		t.Fatalf("expected AI service called once, got %d", aiSvc.calls)
	}
	if aiSvc.captured.Mode != ai.GatewayModeEngineer {
		t.Fatalf("expected engineer mode, got %q", aiSvc.captured.Mode)
	}
	if aiSvc.captured.Input.ContractVersion != ai.ContractVersionV1 || aiSvc.captured.Input.Session.SessionID != "session-1" {
		t.Fatalf("unexpected backend-built input: %+v", aiSvc.captured.Input)
	}
	if len(aiSvc.captured.Input.Events) != 3 {
		t.Fatalf("expected 3 selected events in AI input, got %d", len(aiSvc.captured.Input.Events))
	}
	for _, eventID := range []string{"event-4", "event-5", "event-6"} {
		if !gatewayRequestContainsEvent(aiSvc.captured, eventID) {
			t.Fatalf("expected AI input to include %s, got %+v", eventID, aiSvc.captured.Input.Events)
		}
	}

	var response raceEngineerAdviceResponse
	decodeAdviceResponse(t, recorder, &response)
	if response.SessionID != "session-1" || response.Status != ai.StatusSuccess || response.Message == "" {
		t.Fatalf("unexpected success response: %+v", response)
	}
	if response.Window.MaxEvents != 3 || response.Window.SelectedEventCount != 3 || !response.Window.ProviderCalled {
		t.Fatalf("unexpected window: %+v", response.Window)
	}
	if response.Window.FromUnixMs != 1720656120000 || response.Window.ToUnixMs != 1720656180000 {
		t.Fatalf("expected newest matching event window, got %+v", response.Window)
	}
	if got := strings.Join(response.ReferencedEvents, ","); got != "event-4,event-5,event-6" {
		t.Fatalf("unexpected referenced events: %s", got)
	}
	if response.ProviderInfo.Model == "" || response.GeneratedAtUnixMs <= 0 {
		t.Fatalf("expected provider info and generated timestamp, got %+v", response)
	}
}

func TestRaceEngineerAdviceNoEventsSkipsProvider(t *testing.T) {
	eventStore := events.NewStore(100, events.DedupOptions{})
	seedAdviceEvents(t, eventStore, "session-1", 2)
	aiSvc := &spyAdviceAIService{}
	handler := testRaceEngineerAdviceHandler(t, eventStore, aiSvc, "session-1")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/race-engineer/advice", strings.NewReader(`{"sinceUnixMs":9999999999999}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if aiSvc.calls != 0 {
		t.Fatalf("expected no AI call for empty window, got %d", aiSvc.calls)
	}
	var response raceEngineerAdviceResponse
	decodeAdviceResponse(t, recorder, &response)
	if response.Status != raceEngineerAdviceStatusNoEvents || response.Message != raceEngineerNoEventsMessage {
		t.Fatalf("unexpected no-events response: %+v", response)
	}
	if len(response.ReferencedEvents) != 0 || response.Window.ProviderCalled || response.Window.SelectedEventCount != 0 {
		t.Fatalf("unexpected no-events metadata: %+v", response)
	}
}

func TestRaceEngineerAdviceDefaultsToNewestFive(t *testing.T) {
	eventStore := events.NewStore(100, events.DedupOptions{})
	seedAdviceEvents(t, eventStore, "session-1", 6)
	aiSvc := &spyAdviceAIService{}
	handler := testRaceEngineerAdviceHandler(t, eventStore, aiSvc, "session-1")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/race-engineer/advice", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if aiSvc.calls != 1 || len(aiSvc.captured.Input.Events) != 5 {
		t.Fatalf("expected one AI call with default 5 events, calls=%d events=%d", aiSvc.calls, len(aiSvc.captured.Input.Events))
	}
	if gatewayRequestContainsEvent(aiSvc.captured, "event-1") {
		t.Fatalf("expected newest five events, but event-1 was selected")
	}
	var response raceEngineerAdviceResponse
	decodeAdviceResponse(t, recorder, &response)
	if response.Window.MaxEvents != 5 || response.Window.SelectedEventCount != 5 {
		t.Fatalf("expected default maxEvents 5, got %+v", response.Window)
	}
}

func TestRaceEngineerAdviceMaxEventsClamps(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		seedCount    int
		wantMax      int
		wantSelected int
	}{
		{name: "below range", body: `{"maxEvents":0}`, seedCount: 3, wantMax: 1, wantSelected: 1},
		{name: "above range", body: `{"maxEvents":99}`, seedCount: 12, wantMax: 10, wantSelected: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventStore := events.NewStore(100, events.DedupOptions{})
			seedAdviceEvents(t, eventStore, "session-1", tt.seedCount)
			aiSvc := &spyAdviceAIService{}
			handler := testRaceEngineerAdviceHandler(t, eventStore, aiSvc, "session-1")

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/race-engineer/advice", strings.NewReader(tt.body))
			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d body %s", http.StatusOK, recorder.Code, recorder.Body.String())
			}
			var response raceEngineerAdviceResponse
			decodeAdviceResponse(t, recorder, &response)
			if response.Window.MaxEvents != tt.wantMax || response.Window.SelectedEventCount != tt.wantSelected || len(aiSvc.captured.Input.Events) != tt.wantSelected {
				t.Fatalf("unexpected clamp result response=%+v capturedEvents=%d", response.Window, len(aiSvc.captured.Input.Events))
			}
		})
	}
}

func TestRaceEngineerAdviceRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "malformed JSON", body: `{invalid json}`, wantStatus: http.StatusBadRequest},
		{name: "unknown field", body: `{"unknown":true}`, wantStatus: http.StatusBadRequest},
		{name: "invalid maxEvents type", body: `{"maxEvents":"many"}`, wantStatus: http.StatusBadRequest},
		{name: "invalid sinceUnixMs zero", body: `{"sinceUnixMs":0}`, wantStatus: http.StatusBadRequest},
		{name: "invalid sinceUnixMs negative", body: `{"sinceUnixMs":-1}`, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventStore := events.NewStore(100, events.DedupOptions{})
			aiSvc := &spyAdviceAIService{}
			handler := testRaceEngineerAdviceHandler(t, eventStore, aiSvc, "session-1")

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/race-engineer/advice", strings.NewReader(tt.body))
			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d body %s", tt.wantStatus, recorder.Code, recorder.Body.String())
			}
			if aiSvc.calls != 0 {
				t.Fatalf("expected no AI call for invalid request, got %d", aiSvc.calls)
			}
			if !strings.Contains(recorder.Body.String(), "bad_request") {
				t.Fatalf("expected bad_request envelope, got %s", recorder.Body.String())
			}
		})
	}
}

func TestRaceEngineerAdviceRejectsMissingSessionWithoutProviderCall(t *testing.T) {
	eventStore := events.NewStore(100, events.DedupOptions{})
	aiSvc := &spyAdviceAIService{}
	handler := testRaceEngineerAdviceHandler(t, eventStore, aiSvc, "session-1")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/missing-session/race-engineer/advice", strings.NewReader(`{}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d body %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
	if aiSvc.calls != 0 {
		t.Fatalf("expected no AI call for missing session, got %d", aiSvc.calls)
	}
	if !strings.Contains(recorder.Body.String(), "session_not_found") {
		t.Fatalf("expected session_not_found envelope, got %s", recorder.Body.String())
	}
}

func TestRaceEngineerAdviceMapsProviderFailureStatus(t *testing.T) {
	eventStore := events.NewStore(100, events.DedupOptions{})
	seedAdviceEvents(t, eventStore, "session-1", 1)
	aiSvc := &spyAdviceAIService{resp: ai.GatewayResponse{
		Summary:          "AI analysis is currently unavailable (provider_error).",
		ReferencedEvents: []string{"event-1"},
		ProviderInfo:     ai.ProviderResultInfo{FinishReason: ai.StatusProviderError},
		Status:           ai.StatusProviderError,
		Error:            "provider unavailable",
	}}
	handler := testRaceEngineerAdviceHandler(t, eventStore, aiSvc, "session-1")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/race-engineer/advice", strings.NewReader(`{}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	var response raceEngineerAdviceResponse
	decodeAdviceResponse(t, recorder, &response)
	if response.Status != ai.StatusProviderError || response.Message == "" || !response.Window.ProviderCalled {
		t.Fatalf("unexpected provider failure mapping: %+v", response)
	}
}

func TestRaceEngineerAdviceDirectAIErrorUsesBadRequestEnvelope(t *testing.T) {
	eventStore := events.NewStore(100, events.DedupOptions{})
	seedAdviceEvents(t, eventStore, "session-1", 1)
	aiSvc := &spyAdviceAIService{err: errors.New("gateway failed")}
	handler := testRaceEngineerAdviceHandler(t, eventStore, aiSvc, "session-1")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/race-engineer/advice", strings.NewReader(`{}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d body %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "gateway failed") {
		t.Fatalf("expected gateway error in existing bad_request envelope, got %s", recorder.Body.String())
	}
}

func TestRaceEngineerAdviceListFailureUsesInternalServerError(t *testing.T) {
	aiSvc := &spyAdviceAIService{}
	handler := testRaceEngineerAdviceHandler(t, failingEventStore{}, aiSvc, "session-1")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/race-engineer/advice", strings.NewReader(`{}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d body %s", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
	if aiSvc.calls != 0 {
		t.Fatalf("expected AI service not called when event listing fails, got %d", aiSvc.calls)
	}
	if !strings.Contains(recorder.Body.String(), "internal_error") {
		t.Fatalf("expected internal_error envelope, got %s", recorder.Body.String())
	}
}

func TestBuildRaceEngineerAdviceGatewayRequestUsesFirstAvailableSessionRefs(t *testing.T) {
	track := &events.CatalogRef{ID: apiStringPtr("track-1"), Name: apiStringPtr("Trial Mountain"), DisplayStrategy: events.DisplayStrategyCatalogName}
	layout := &events.CatalogRef{ID: apiStringPtr("layout-1"), Name: apiStringPtr("Forward"), DisplayStrategy: events.DisplayStrategyCatalogName}

	req := buildRaceEngineerAdviceGatewayRequest("session-1", []events.EngineerEvent{
		{EventID: "event-1", SessionID: "session-1"},
		{EventID: "event-2", SessionID: "session-1", Track: track, Layout: layout},
	})

	if req.Input.Session.Track != track {
		t.Fatalf("expected session track from first available selected event, got %+v", req.Input.Session.Track)
	}
	if req.Input.Session.Layout != layout {
		t.Fatalf("expected session layout from first available selected event, got %+v", req.Input.Session.Layout)
	}
}

func testRaceEngineerAdviceHandler(t *testing.T, eventStore events.Repository, aiSvc ai.AIService, sessionIDs ...string) http.Handler {
	t.Helper()
	cfg := config.Config{Addr: ":0", Env: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	sessionRepo := sessions.NewMemoryRepository()
	for _, sessionID := range sessionIDs {
		seedTestSession(t, sessionRepo, sessionID)
	}
	statsRepo := admin.NewMemoryStatsRepo(sessionRepo, frameStore)
	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, frameStore, eventStore)
	return routesWithAIAndSessions(cfg, logger, frameStore, catalog, eventStore, sessionRepo, aiSvc, statsRepo, summaryRepo)
}

func seedAdviceEvents(t *testing.T, store *events.Store, sessionID string, count int) {
	t.Helper()
	for i := 1; i <= count; i++ {
		index := i
		seedAPIEvent(t, store, apiEngineerEvent(func(event *events.EngineerEvent) {
			event.EventID = "event-" + strconv.Itoa(index)
			event.SessionID = sessionID
			event.TimestampUnixMs = 1720656000000 + int64(index)*30000
			event.TimeRange = &events.TimeRange{StartUnixMs: event.TimestampUnixMs - 1000, EndUnixMs: event.TimestampUnixMs}
			event.LapNumber = index
			event.Corner = &events.CatalogRef{ID: apiStringPtr("corner-" + strconv.Itoa(index)), DisplayStrategy: events.DisplayStrategyIDOnly}
		}))
	}
}

func decodeAdviceResponse(t *testing.T, recorder *httptest.ResponseRecorder, response *raceEngineerAdviceResponse) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), response); err != nil {
		t.Fatalf("failed to decode advice response: %v body %s", err, recorder.Body.String())
	}
}

func referencedEventIDsFromGateway(req ai.GatewayRequest) []string {
	ids := make([]string, len(req.Input.Events))
	for i, envelope := range req.Input.Events {
		ids[i] = envelope.Event.EventID
	}
	return ids
}

func gatewayRequestContainsEvent(req ai.GatewayRequest, eventID string) bool {
	for _, envelope := range req.Input.Events {
		if envelope.Event.EventID == eventID {
			return true
		}
	}
	return false
}
