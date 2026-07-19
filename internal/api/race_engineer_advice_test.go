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
	"telemetry-one-backend/internal/laps"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

type productionAdviceAdapter struct {
	result ai.ProviderResponse
}

func (a *productionAdviceAdapter) Analyze(_ context.Context, _ ai.ProviderRequest) (ai.ProviderResponse, error) {
	return a.result, nil
}

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

	assertAPIVersion(t, decodeJSONMap(t, recorder.Body.Bytes()))
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

func TestRaceEngineerAdviceUsesDerivedSignalsWithoutGeometry(t *testing.T) {
	eventStore := events.NewStore(100, events.DedupOptions{})
	seedAPIEvent(t, eventStore, apiEngineerEvent(func(event *events.EngineerEvent) {
		event.EventID = "event-offtrack"
		event.SessionID = "session-signal"
		event.Type = events.TypeOffTrackStint
		event.Severity = events.SeverityMedium
		event.TimestampUnixMs = 1720656180000
		event.TimeRange = &events.TimeRange{StartUnixMs: event.TimestampUnixMs - 1000, EndUnixMs: event.TimestampUnixMs}
		event.LapNumber = 3
	}))
	lapRepo := laps.NewMemoryRepository()
	ctx := context.Background()
	if err := lapRepo.UpsertCompleted(ctx, []laps.CompletedLap{
		{SessionID: "session-signal", LapNumber: 1, LapTimeMs: 92100, CompletedAtUnixMs: 1720656000000, TelemetryGapCount: 0},
		{SessionID: "session-signal", LapNumber: 2, LapTimeMs: 97800, CompletedAtUnixMs: 1720656060000, TelemetryGapCount: 2},
		{SessionID: "session-signal", LapNumber: 3, LapTimeMs: 100400, CompletedAtUnixMs: 1720656120000, TelemetryGapCount: 1},
	}); err != nil {
		t.Fatalf("seed completed laps: %v", err)
	}
	adapter := &productionAdviceAdapter{result: ai.ProviderResponse{Content: "Keep pushing the exits and protect the tyres.", Model: "test-model", FinishReason: "stop", Usage: ai.UsageInfo{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}}
	controller := &ai.Controller{Budget: ai.DefaultTokenBudget(), Retry: ai.DefaultRetryPolicy()}
	aiSvc := ai.NewService(adapter, controller)
	cfg := config.Config{Addr: ":0", Env: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-signal")
	statsRepo := admin.NewMemoryStatsRepo(sessionRepo, frameStore)
	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, frameStore, eventStore)
	handler := routesWithAIAndSessionsAndLaps(cfg, logger, frameStore, catalog, eventStore, lapRepo, lapRepo, sessionRepo, aiSvc, statsRepo, summaryRepo)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-signal/race-engineer/advice", strings.NewReader(`{}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	var response raceEngineerAdviceResponse
	decodeAdviceResponse(t, recorder, &response)
	if response.Status != ai.StatusSuccess || response.Window.ProviderCalled != true || response.Window.DerivedSignalCount == 0 {
		t.Fatalf("expected successful advice with derived signals, got %+v", response)
	}
	if len(response.Signals) != 3 {
		t.Fatalf("expected all recent signals in response, got %+v", response.Signals)
	}
	for _, kind := range []string{"lap_pace_regression", "telemetry_gap_warning", "off_track_stint_warning"} {
		if !signalKindsContain(response.Signals, kind) {
			t.Fatalf("expected signal %s in response, got %+v", kind, response.Signals)
		}
	}
	if response.ProviderInfo.Model != "test-model" || response.Message == "" {
		t.Fatalf("expected production AI service response, got %+v", response)
	}
}

func TestRaceEngineerAdviceFiltersStaleDerivedSignalsBySince(t *testing.T) {
	eventStore := events.NewStore(100, events.DedupOptions{})
	seedAPIEvent(t, eventStore, apiEngineerEvent(func(event *events.EngineerEvent) {
		event.EventID = "event-1"
		event.SessionID = "session-window"
		event.TimestampUnixMs = 1720656000000
		event.TimeRange = &events.TimeRange{StartUnixMs: event.TimestampUnixMs - 1000, EndUnixMs: event.TimestampUnixMs}
		event.LapNumber = 1
	}))
	seedAPIEvent(t, eventStore, apiEngineerEvent(func(event *events.EngineerEvent) {
		event.EventID = "event-2"
		event.SessionID = "session-window"
		event.TimestampUnixMs = 1720656300000
		event.TimeRange = &events.TimeRange{StartUnixMs: event.TimestampUnixMs - 1000, EndUnixMs: event.TimestampUnixMs}
		event.LapNumber = 2
	}))

	lapRepo := laps.NewMemoryRepository()
	if err := lapRepo.UpsertCompleted(context.Background(), []laps.CompletedLap{
		{SessionID: "session-window", LapNumber: 1, LapTimeMs: 91000, CompletedAtUnixMs: 1720656000000, TelemetryGapCount: 4},
		{SessionID: "session-window", LapNumber: 2, LapTimeMs: 94000, CompletedAtUnixMs: 1720656300000, TelemetryGapCount: 0},
	}); err != nil {
		t.Fatalf("seed laps: %v", err)
	}

	adapter := &productionAdviceAdapter{result: ai.ProviderResponse{Content: "Recent window only.", Model: "test-model", FinishReason: "stop", Usage: ai.UsageInfo{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}}
	controller := &ai.Controller{Budget: ai.DefaultTokenBudget(), Retry: ai.DefaultRetryPolicy()}
	aiSvc := ai.NewService(adapter, controller)
	cfg := config.Config{Addr: ":0", Env: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-window")
	statsRepo := admin.NewMemoryStatsRepo(sessionRepo, frameStore)
	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, frameStore, eventStore)
	handler := routesWithAIAndSessionsAndLaps(cfg, logger, frameStore, catalog, eventStore, lapRepo, lapRepo, sessionRepo, aiSvc, statsRepo, summaryRepo)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-window/race-engineer/advice", strings.NewReader(`{"sinceUnixMs":1720656200000}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	var response raceEngineerAdviceResponse
	decodeAdviceResponse(t, recorder, &response)
	if response.Window.DerivedSignalCount != 0 {
		t.Fatalf("expected stale laps and off-track events to be filtered out, got %+v", response.Window)
	}
	if len(response.Signals) != 0 {
		t.Fatalf("expected no derived signals in recent window, got %+v", response.Signals)
	}
	if response.Window.SelectedEventCount != 1 || len(response.ReferencedEvents) != 1 || response.ReferencedEvents[0] != "event-2" {
		t.Fatalf("expected only the recent event to survive the window, got %+v", response)
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

func TestBuildRaceEngineerAdviceGatewayRequestUsesAtomicEventRefs(t *testing.T) {
	track := &events.CatalogRef{ID: apiStringPtr("track-1"), Name: apiStringPtr("Trial Mountain"), DisplayStrategy: events.DisplayStrategyCatalogName}
	layout := &events.CatalogRef{ID: apiStringPtr("layout-1"), Name: apiStringPtr("Forward"), DisplayStrategy: events.DisplayStrategyCatalogName}
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()

	req := buildRaceEngineerAdviceGatewayRequest(context.Background(), sessions.Session{ID: "session-1"}, []events.EngineerEvent{
		{EventID: "event-1", SessionID: "session-1"},
		{EventID: "event-2", SessionID: "session-1", Track: track, Layout: layout},
	}, frameStore, catalog)

	if req.Input.Session.Track != track {
		t.Fatalf("expected session track from first available selected event, got %+v", req.Input.Session.Track)
	}
	if req.Input.Session.Layout != layout {
		t.Fatalf("expected session layout from first available selected event, got %+v", req.Input.Session.Layout)
	}
}

func TestHasLapOneBeganFalseForEmptyFrames(t *testing.T) {
	if hasLapOneBegan(nil) {
		t.Fatal("expected false for nil frames")
	}
	if hasLapOneBegan([]telemetry.Frame{}) {
		t.Fatal("expected false for empty frames")
	}
}

func TestHasLapOneBeganFalseForPreRaceFrames(t *testing.T) {
	frames := []telemetry.Frame{
		{LapNumber: 0, TimestampUnixMs: 1000},
		{LapNumber: 0, TimestampUnixMs: 2000},
	}
	if hasLapOneBegan(frames) {
		t.Fatal("expected false when all frames have LapNumber 0")
	}
}

func TestHasLapOneBeganTrueWhenLapOnePresent(t *testing.T) {
	frames := []telemetry.Frame{
		{LapNumber: 0, TimestampUnixMs: 1000},
		{LapNumber: 1, TimestampUnixMs: 2000},
	}
	if !hasLapOneBegan(frames) {
		t.Fatal("expected true when any frame has LapNumber >= 1")
	}
}

func TestHasLapOneBeganTrueForLaterLaps(t *testing.T) {
	frames := []telemetry.Frame{
		{LapNumber: 3, TimestampUnixMs: 1000},
	}
	if !hasLapOneBegan(frames) {
		t.Fatal("expected true for lap 3")
	}
}

func TestBuildRaceEngineerAdviceGatewayRequestDoesNotDetectBeforeLapOne(t *testing.T) {
	frames := make([]telemetry.Frame, 35)
	for i := range frames {
		frames[i] = telemetry.Frame{
			TimestampUnixMs: int64(i + 1),
			LapNumber:       0,
			IsOnTrack:       true,
		}
	}
	frameStore := telemetry.NewFrameStore(100)
	if err := frameStore.Append(context.Background(), "session-prerace", frames); err != nil {
		t.Fatalf("append frames: %v", err)
	}
	catalog := tracks.OfficialGT7SeedCatalog()

	inputEvents := []events.EngineerEvent{
		{
			EventID: "event-prerace", SessionID: "session-prerace",
			Version: events.ContractVersionV1, Type: events.TypeLapTimeRegression,
			Severity: events.SeverityMedium, Confidence: 0.85,
			LapNumber: 0, TimestampUnixMs: 1720656000000,
			Source:  events.EventSource{Kind: events.SourceDeterministicRule, RuleID: "test.v1", RuleVersion: "v1"},
			Metrics: []events.MetricEvidence{{Name: "testMetric", Value: 1, Status: events.MetricStatusAvailable, Role: events.MetricRoleActual}},
		},
	}

	req := buildRaceEngineerAdviceGatewayRequest(context.Background(), sessions.Session{ID: "session-prerace"}, inputEvents, frameStore, catalog)

	if req.Input.Session.Track != nil {
		t.Fatalf("expected nil track before lap 1 begins, got %+v", req.Input.Session.Track)
	}
	if req.Input.Session.Layout != nil {
		t.Fatalf("expected nil layout before lap 1 begins, got %+v", req.Input.Session.Layout)
	}
}

func TestBuildRaceEngineerAdviceGatewayRequestFallsBackToDetectionWhenEventsLackRefs(t *testing.T) {
	const lengthMeters = 5423.0
	const count = 34
	frames := apiStraightCompletedLapFrames(lengthMeters, count)
	frameStore := telemetry.NewFrameStore(100)
	if err := frameStore.Append(context.Background(), "session-detect-fallback", frames); err != nil {
		t.Fatalf("append frames: %v", err)
	}
	catalog := tracks.OfficialGT7SeedCatalog()

	inputEvents := []events.EngineerEvent{
		{
			EventID: "event-no-refs", SessionID: "session-detect-fallback",
			Version: events.ContractVersionV1, Type: events.TypeLapTimeRegression,
			Severity: events.SeverityMedium, Confidence: 0.85,
			LapNumber: 2, TimestampUnixMs: 1720656000000,
			Source:  events.EventSource{Kind: events.SourceDeterministicRule, RuleID: "test.v1", RuleVersion: "v1"},
			Metrics: []events.MetricEvidence{{Name: "testMetric", Value: 1, Status: events.MetricStatusAvailable, Role: events.MetricRoleActual}},
		},
	}

	req := buildRaceEngineerAdviceGatewayRequest(context.Background(), sessions.Session{ID: "session-detect-fallback"}, inputEvents, frameStore, catalog)

	if req.Input.Session.Track == nil || *req.Input.Session.Track.ID != "gt7_watkins_glen_international" {
		t.Fatalf("expected fallback track detection to find Watkins Glen, got %+v", req.Input.Session.Track)
	}
	if req.Input.Session.Layout == nil || *req.Input.Session.Layout.ID != "gt7_layout_1240" {
		t.Fatalf("expected fallback layout detection to find Long Course, got %+v", req.Input.Session.Layout)
	}
	if req.Input.Session.Track.DisplayStrategy != events.DisplayStrategyCatalogName {
		t.Fatalf("expected catalog_name display strategy for fallback track ref, got %s", req.Input.Session.Track.DisplayStrategy)
	}
}

func TestBuildRaceEngineerAdviceGatewayRequestUsesPersistedSessionRefsWhenEventsLackRefs(t *testing.T) {
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	session := sessions.Session{ID: "session-persisted", DetectedTrackID: "gt7_watkins_glen_international", DetectedLayoutID: "gt7_layout_1240"}

	req := buildRaceEngineerAdviceGatewayRequest(context.Background(), session, []events.EngineerEvent{{EventID: "event-no-refs", SessionID: session.ID}}, frameStore, catalog)

	if req.Input.Session.Track == nil || req.Input.Session.Layout == nil {
		t.Fatalf("expected persisted session refs to populate track and layout, got %+v", req.Input.Session)
	}
	if got := *req.Input.Session.Track.ID; got != session.DetectedTrackID {
		t.Fatalf("expected persisted track id %s, got %s", session.DetectedTrackID, got)
	}
	if got := *req.Input.Session.Layout.ID; got != session.DetectedLayoutID {
		t.Fatalf("expected persisted layout id %s, got %s", session.DetectedLayoutID, got)
	}
	if got := *req.Input.Session.Track.Name; got != "Watkins Glen International" {
		t.Fatalf("expected resolved track name from catalog, got %s", got)
	}
	if got := *req.Input.Session.Layout.Name; got != "Watkins Glen Long Course" {
		t.Fatalf("expected resolved layout name from catalog, got %s", got)
	}
}

func TestBuildRaceEngineerAdviceGatewayRequestFallsBackToManualTrackWhenLayoutUnknown(t *testing.T) {
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	session := sessions.Session{ID: "session-manual-track", TrackID: "gt7_watkins_glen_international"}

	req := buildRaceEngineerAdviceGatewayRequest(context.Background(), session, []events.EngineerEvent{{EventID: "event-no-refs", SessionID: session.ID}}, frameStore, catalog)

	if req.Input.Session.Track == nil {
		t.Fatalf("expected manual track ref to populate session track, got %+v", req.Input.Session)
	}
	if got := *req.Input.Session.Track.ID; got != session.TrackID {
		t.Fatalf("expected manual track id %s, got %s", session.TrackID, got)
	}
	if got := *req.Input.Session.Track.Name; got != "Watkins Glen International" {
		t.Fatalf("expected manual track name from catalog, got %s", got)
	}
	if req.Input.Session.Layout != nil {
		t.Fatalf("expected layout to remain unknown for manual track-only session, got %+v", req.Input.Session.Layout)
	}
	if req.Input.Session.Track.DisplayStrategy != events.DisplayStrategyCatalogName {
		t.Fatalf("expected catalog_name display strategy for manual track ref, got %s", req.Input.Session.Track.DisplayStrategy)
	}
}

func TestBuildRaceEngineerAdviceGatewayRequestPrefersAtomicEventRefsOverPersistedSessionRefs(t *testing.T) {
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	session := sessions.Session{ID: "session-event-priority", DetectedTrackID: "gt7_trial_mountain_circuit", DetectedLayoutID: "gt7_layout_1024"}
	eventTrack := &events.CatalogRef{ID: apiStringPtr("gt7_watkins_glen_international"), Name: apiStringPtr("Watkins Glen International"), DisplayStrategy: events.DisplayStrategyCatalogName}
	eventLayout := &events.CatalogRef{ID: apiStringPtr("gt7_layout_1240"), Name: apiStringPtr("Watkins Glen Long Course"), DisplayStrategy: events.DisplayStrategyCatalogName}

	req := buildRaceEngineerAdviceGatewayRequest(context.Background(), session, []events.EngineerEvent{{EventID: "event-with-refs", SessionID: session.ID, Track: eventTrack, Layout: eventLayout}}, frameStore, catalog)

	if req.Input.Session.Track != eventTrack || req.Input.Session.Layout != eventLayout {
		t.Fatalf("expected selected event refs to win over persisted session refs, got track=%+v layout=%+v", req.Input.Session.Track, req.Input.Session.Layout)
	}
}

func TestBuildRaceEngineerAdviceGatewayRequestPrefersDetectedPairOverManualTrackOnly(t *testing.T) {
	const lengthMeters = 5423.0
	const count = 34
	frames := apiStraightCompletedLapFrames(lengthMeters, count)
	frameStore := telemetry.NewFrameStore(100)
	if err := frameStore.Append(context.Background(), "session-manual-track-detectable", frames); err != nil {
		t.Fatalf("append frames: %v", err)
	}
	catalog := tracks.OfficialGT7SeedCatalog()
	session := sessions.Session{ID: "session-manual-track-detectable", TrackID: "gt7_watkins_glen_international"}

	req := buildRaceEngineerAdviceGatewayRequest(context.Background(), session, []events.EngineerEvent{{EventID: "event-no-refs", SessionID: session.ID, LapNumber: 2, TimestampUnixMs: 1720656000000, Version: events.ContractVersionV1, Type: events.TypeLapTimeRegression, Severity: events.SeverityMedium, Confidence: 0.85, Source: events.EventSource{Kind: events.SourceDeterministicRule, RuleID: "test.v1", RuleVersion: "v1"}, Metrics: []events.MetricEvidence{{Name: "testMetric", Value: 1, Status: events.MetricStatusAvailable, Role: events.MetricRoleActual}}}}, frameStore, catalog)

	if req.Input.Session.Track == nil || req.Input.Session.Layout == nil {
		t.Fatalf("expected detected pair to win over manual track-only fallback, got %+v", req.Input.Session)
	}
	if got := *req.Input.Session.Track.ID; got != "gt7_watkins_glen_international" {
		t.Fatalf("expected detected track id, got %s", got)
	}
	if got := *req.Input.Session.Layout.ID; got != "gt7_layout_1240" {
		t.Fatalf("expected detected layout id, got %s", got)
	}
}

func TestBuildRaceEngineerAdviceGatewayRequestFallsBackToDetectionWhenPersistedRefsMissingOrUnresolvable(t *testing.T) {
	const lengthMeters = 5423.0
	const count = 34
	frames := apiStraightCompletedLapFrames(lengthMeters, count)
	frameStore := telemetry.NewFrameStore(100)
	if err := frameStore.Append(context.Background(), "session-persisted-fallback", frames); err != nil {
		t.Fatalf("append frames: %v", err)
	}
	catalog := tracks.OfficialGT7SeedCatalog()

	tests := []struct {
		name                  string
		session               sessions.Session
		wantPersistedTrackID  string
		wantPersistedLayoutID string
		wantDetectedTrackID   string
		wantDetectedLayoutID  string
	}{
		{name: "missing layout id", session: sessions.Session{ID: "session-persisted-fallback", DetectedTrackID: "gt7_trial_mountain_circuit"}, wantPersistedTrackID: "gt7_trial_mountain_circuit", wantPersistedLayoutID: "gt7_layout_1024", wantDetectedTrackID: "gt7_watkins_glen_international", wantDetectedLayoutID: "gt7_layout_1240"},
		{name: "missing track id", session: sessions.Session{ID: "session-persisted-fallback", DetectedLayoutID: "gt7_layout_1024"}, wantPersistedTrackID: "gt7_trial_mountain_circuit", wantPersistedLayoutID: "gt7_layout_1024", wantDetectedTrackID: "gt7_watkins_glen_international", wantDetectedLayoutID: "gt7_layout_1240"},
		{name: "unresolvable ids", session: sessions.Session{ID: "session-persisted-fallback", DetectedTrackID: "unknown-track", DetectedLayoutID: "unknown-layout"}, wantDetectedTrackID: "gt7_watkins_glen_international", wantDetectedLayoutID: "gt7_layout_1240"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := buildRaceEngineerAdviceGatewayRequest(context.Background(), tt.session, []events.EngineerEvent{{EventID: "event-no-refs", SessionID: tt.session.ID, LapNumber: 2, TimestampUnixMs: 1720656000000, Version: events.ContractVersionV1, Type: events.TypeLapTimeRegression, Severity: events.SeverityMedium, Confidence: 0.85, Source: events.EventSource{Kind: events.SourceDeterministicRule, RuleID: "test.v1", RuleVersion: "v1"}, Metrics: []events.MetricEvidence{{Name: "testMetric", Value: 1, Status: events.MetricStatusAvailable, Role: events.MetricRoleActual}}}}, frameStore, catalog)

			if req.Input.Session.Track == nil || req.Input.Session.Layout == nil {
				t.Fatalf("expected fallback to resolve a complete track/layout pair, got %+v", req.Input.Session)
			}

			gotTrackID := *req.Input.Session.Track.ID
			gotLayoutID := *req.Input.Session.Layout.ID

			if tt.wantPersistedTrackID != "" && tt.wantPersistedLayoutID != "" {
				if gotTrackID == tt.wantPersistedTrackID && gotLayoutID == tt.wantDetectedLayoutID {
					t.Fatalf("unexpected hybrid refs: persisted track %s with detected layout %s", gotTrackID, gotLayoutID)
				}
				if gotTrackID == tt.wantDetectedTrackID && gotLayoutID == tt.wantPersistedLayoutID {
					t.Fatalf("unexpected hybrid refs: detected track %s with persisted layout %s", gotTrackID, gotLayoutID)
				}
			}

			switch {
			case gotTrackID == tt.wantPersistedTrackID && gotLayoutID == tt.wantPersistedLayoutID:
			case gotTrackID == tt.wantDetectedTrackID && gotLayoutID == tt.wantDetectedLayoutID:
			default:
				t.Fatalf("expected complete persisted pair %s/%s or complete detected pair %s/%s, got track=%+v layout=%+v", tt.wantPersistedTrackID, tt.wantPersistedLayoutID, tt.wantDetectedTrackID, tt.wantDetectedLayoutID, req.Input.Session.Track, req.Input.Session.Layout)
			}
		})
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

func gatewayRequestContainsSignal(req ai.GatewayRequest, kind string) bool {
	for _, signal := range req.Input.Signals {
		if signal.Kind == kind {
			return true
		}
	}
	return false
}

func signalKindsContain(signals []ai.Signal, kind string) bool {
	for _, signal := range signals {
		if signal.Kind == kind {
			return true
		}
	}
	return false
}
