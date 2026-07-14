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

type testAIService struct {
	resp ai.GatewayResponse
	err  error
}

func (s *testAIService) Analyze(_ context.Context, _ ai.GatewayRequest) (ai.GatewayResponse, error) {
	return s.resp, s.err
}

const validAnalyzePayload = `{
	"input":{
		"contractVersion":"telemetry-one.ai-consumer-input.v1",
		"session":{
			"sessionId":"test-session",
			"track":{"id":"track-1","name":"Test Track","displayStrategy":"catalog_name"},
			"layout":{"id":"layout-1","name":"Test Layout","displayStrategy":"catalog_name"}
		},
		"events":[{
			"event":{
				"eventId":"event-1",
				"sessionId":"test-session",
				"version":"telemetry-one.engineer-event.v1",
				"type":"late_throttle",
				"severity":"medium",
				"confidence":0.82,
				"timestampUnixMs":1720656012345,
				"lapNumber":2,
				"track":{"id":"track-1","name":"Test Track","displayStrategy":"catalog_name"},
				"layout":{"id":"layout-1","name":"Test Layout","displayStrategy":"catalog_name"},
				"corner":{"id":"corner-1","name":"Test Corner","displayStrategy":"catalog_name"},
				"metrics":[{"name":"throttleReapplicationDeltaMs","value":320,"unit":"ms","status":"available","role":"delta"}],
				"source":{"kind":"deterministic_rule","ruleId":"late_throttle.v1","ruleVersion":"v1"}
			}
		}],
		"safety":{
			"redactionPolicy":"no raw telemetry",
			"allowedInputKinds":["engineer_events","derived_metrics","catalog_refs","session_context","unknown_states_explicit"]
		},
		"constraints":["test constraint"]
	},
	"mode":"engineer"
}`

func TestAnalyzeEndpointAcceptsValidRequest(t *testing.T) {
	aiSvc := &testAIService{
		resp: ai.GatewayResponse{
			Summary: "Session test-session analyzed",
			EventExplanations: []ai.EventExplanation{
				{EventID: "event-1", Type: events.TypeLateThrottle, Explanation: "late throttle exit", Relevance: 0.85},
			},
			Recommendations:  []string{"work on earlier throttle"},
			ReferencedEvents: []string{"event-1"},
			ProviderInfo: ai.ProviderResultInfo{
				Model:        "test-model",
				FinishReason: "stop",
				Usage:        ai.UsageInfo{PromptTokens: 50, CompletionTokens: 100, TotalTokens: 150},
			},
			Status: "success",
		},
	}
	handler := testAnalyzeHandler(t, aiSvc)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test-session/analyze", strings.NewReader(validAnalyzePayload))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response ai.GatewayResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Summary != "Session test-session analyzed" {
		t.Fatalf("expected summary, got %q", response.Summary)
	}
	if len(response.EventExplanations) != 1 {
		t.Fatalf("expected 1 event explanation, got %d", len(response.EventExplanations))
	}
	if len(response.Recommendations) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(response.Recommendations))
	}
	if response.Status != "success" {
		t.Fatalf("expected success status, got %s", response.Status)
	}
}

func TestAnalyzeEndpointRejectsInvalidJSON(t *testing.T) {
	aiSvc := &testAIService{
		resp: ai.GatewayResponse{Summary: "should not be called"},
	}
	handler := testAnalyzeHandler(t, aiSvc)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test-session/analyze", strings.NewReader(`{invalid json}`))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "invalid JSON body") {
		t.Fatalf("expected invalid JSON body error, got %s", recorder.Body.String())
	}
}

func TestAnalyzeEndpointRejectsMissingContractVersion(t *testing.T) {
	aiSvc := &testAIService{
		err: ai.ErrEmptyGatewayRequest,
	}
	handler := testAnalyzeHandler(t, aiSvc)

	payload := strings.Replace(validAnalyzePayload, `"contractVersion":"telemetry-one.ai-consumer-input.v1"`, `"contractVersion":""`, 1)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test-session/analyze", strings.NewReader(payload))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestAnalyzeEndpointRejectsRawTelemetryFields(t *testing.T) {
	aiSvc := &testAIService{
		resp: ai.GatewayResponse{Summary: "should not be called"},
	}
	handler := testAnalyzeHandler(t, aiSvc)

	payload := strings.Replace(validAnalyzePayload, `"safety":`, `"frames":[{"positionX":123.4}],"safety":`, 1)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test-session/analyze", strings.NewReader(payload))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d for raw telemetry rejection, got %d with body %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestAnalyzeEndpointRejectsUnsupportedMode(t *testing.T) {
	aiSvc := &testAIService{
		err: ai.ErrUnsupportedGatewayMode,
	}
	handler := testAnalyzeHandler(t, aiSvc)

	payload := strings.Replace(validAnalyzePayload, `"mode":"engineer"`, `"mode":"unknown_mode"`, 1)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test-session/analyze", strings.NewReader(payload))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestAnalyzeEndpointAllowsExplicitUnknownCatalogRefsViaJSON(t *testing.T) {
	aiSvc := &testAIService{
		resp: ai.GatewayResponse{
			Summary:          "Unknown track analyzed",
			ReferencedEvents: []string{"event-1"},
			ProviderInfo:     ai.ProviderResultInfo{Model: "test-model", FinishReason: "stop"},
			Status:           "success",
		},
	}
	handler := testAnalyzeHandler(t, aiSvc)

	payload := strings.Replace(validAnalyzePayload,
		`"track":{"id":"track-1","name":"Test Track","displayStrategy":"catalog_name"}`,
		`"track":null`, 1)
	payload = strings.Replace(payload,
		`"layout":{"id":"layout-1","name":"Test Layout","displayStrategy":"catalog_name"}`,
		`"layout":null`, 1)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test-session/analyze", strings.NewReader(payload))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected null catalog refs to be valid, got status %d with body %s", recorder.Code, recorder.Body.String())
	}

	var response ai.GatewayResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !strings.Contains(response.Summary, "Unknown track") {
		t.Fatalf("expected summary with unknown track, got %q", response.Summary)
	}
}

func TestAnalyzeEndpointResponseDoesNotLeakRawTelemetry(t *testing.T) {
	aiSvc := &testAIService{
		resp: ai.GatewayResponse{
			Summary:          "clean response",
			ReferencedEvents: []string{"event-1"},
			ProviderInfo:     ai.ProviderResultInfo{Model: "test-model", FinishReason: "stop"},
			Status:           "success",
		},
	}
	handler := testAnalyzeHandler(t, aiSvc)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test-session/analyze", strings.NewReader(validAnalyzePayload))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body := recorder.Body.String()
	for _, forbidden := range []string{`"positionX":`, `"speedMps":`, `"throttle":`, `"brake":`, `"steering":`, `"frames":`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("expected analyze response not to contain raw telemetry field %q in: %s", forbidden, body)
		}
	}
}

func TestAnalyzeEndpointWrongMethod(t *testing.T) {
	aiSvc := &testAIService{resp: ai.GatewayResponse{Summary: "should not be called"}}
	handler := testAnalyzeHandler(t, aiSvc)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(method, "/api/v1/sessions/test-session/analyze", nil)

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected status %d for %s, got %d", http.StatusMethodNotAllowed, method, recorder.Code)
			}
		})
	}
}

func testAnalyzeHandler(t *testing.T, aiSvc ai.AIService) http.Handler {
	t.Helper()

	cfg := config.Config{Addr: ":0", Env: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	eventStore := events.NewStore(100, events.DedupOptions{})
	sessionRepo := sessions.NewMemoryRepository()
	sessionRepo.Create(context.Background(), sessions.Session{
		ID: "test-session", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: time.UnixMilli(1720656000000).UTC(),
	})

	statsRepo := admin.NewMemoryStatsRepo(sessionRepo, frameStore)
	return routesWithAIAndSessions(cfg, logger, frameStore, catalog, eventStore, sessionRepo, aiSvc, statsRepo)
}
