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

func TestHealthEndpointWithoutPrefix(t *testing.T) {
	handler := routes(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body healthResponse
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if body.Status != "ok" {
		t.Fatalf("expected status ok, got %q", body.Status)
	}

	if body.Env != "test" {
		t.Fatalf("expected env test, got %q", body.Env)
	}
}

func TestIngestFramesWrongMethod(t *testing.T) {
	handler := routes(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(method, "/api/v1/sessions/session-1/frames", nil)

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected status %d for %s, got %d", http.StatusMethodNotAllowed, method, recorder.Code)
			}
		})
	}
}

func TestDetectTrackMissingSessionId(t *testing.T) {
	handler := routes(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions//track", nil)

	handler.ServeHTTP(recorder, request)

	// Go 1.22 ServeMux normalizes paths with empty segments: the double slash
	// triggers a 301 redirect to the cleaned path (/api/v1/sessions/track).
	// The {sessionId} path parameter never matches an empty segment, so the
	// detectTrackHandler's own empty-sessionId guard is never reached through
	// normal HTTP routing.
	if recorder.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301 redirect (path cleaning) for double-slash path, got %d", recorder.Code)
	}

	location := recorder.Header().Get("Location")
	if !strings.HasSuffix(location, "/api/v1/sessions/track") {
		t.Fatalf("expected redirect to cleaned path, got Location: %q", location)
	}
}

func TestUnknownRoute(t *testing.T) {
	handler := routes(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestIngestFramesSessionIdPathParamRequired(t *testing.T) {
	handler := routes(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions//frames", nil)

	handler.ServeHTTP(recorder, request)

	// Must not crash. Go's ServeMux normalizes the double-slash path with a
	// 301 redirect rather than matching any registered route — specifically,
	// it must NOT match the POST /api/v1/sessions/{sessionId}/frames handler.
	if recorder.Code == http.StatusAccepted {
		t.Fatalf("empty sessionId path must not match the POST ingest route, got 202")
	}

	// The mux normalizes // -> / before matching, so this becomes a redirect.
	if recorder.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301 redirect (path cleaning) for double-slash, got %d", recorder.Code)
	}
}

func TestFormatTopReasonsDeterministic(t *testing.T) {
	summary := &telemetry.RejectionSummary{
		Reasons: []telemetry.RejectionReasonCount{
			{Code: "invalid_speed", Count: 5},
			{Code: "invalid_throttle", Count: 3},
			{Code: "invalid_timestamp", Count: 3},
			{Code: "non_monotonic_timestamp", Count: 1},
		},
	}
	result := formatTopReasons(summary)
	expected := []string{"invalid_speed", "invalid_throttle", "invalid_timestamp"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d reasons, got %d: %v", len(expected), len(result), result)
	}
	for i, code := range result {
		if code != expected[i] {
			t.Fatalf("result[%d]: expected %q, got %q", i, expected[i], code)
		}
	}
}

func TestFormatTopReasonsNil(t *testing.T) {
	if result := formatTopReasons(nil); result != nil {
		t.Fatalf("expected nil for nil summary, got %v", result)
	}
}

func TestFormatTopReasonsEmpty(t *testing.T) {
	result := formatTopReasons(&telemetry.RejectionSummary{})
	if result != nil {
		t.Fatalf("expected nil for empty summary, got %v", result)
	}
}

func TestFormatTopReasonsUnderLimit(t *testing.T) {
	summary := &telemetry.RejectionSummary{
		Reasons: []telemetry.RejectionReasonCount{
			{Code: "a_code", Count: 2},
			{Code: "b_code", Count: 1},
		},
	}
	result := formatTopReasons(summary)
	if len(result) != 2 {
		t.Fatalf("expected 2 reasons when under limit, got %d: %v", len(result), result)
	}
}

func TestIngestFramesRejectsNonexistentSession(t *testing.T) {
	sessionRepo := sessions.NewMemoryRepository()
	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		telemetry.NewFrameStore(100),
		tracks.OfficialGT7SeedCatalog(),
		events.NewStore(100, events.DedupOptions{}),
		sessionRepo,
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session_nonexistent/frames", strings.NewReader(`{"frames":[{"timestampUnixMs":1,"speedMps":50,"rpm":5000,"gear":3,"throttle":0.5,"brake":0,"steering":0,"fuelLiters":30,"positionX":0,"positionY":0,"positionZ":0,"lapNumber":1,"currentLapMs":50000,"isOnTrack":true}]}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "session_not_found") {
		t.Fatalf("expected session_not_found, got %s", recorder.Body.String())
	}
}

func TestIngestFramesRejectsFinishedSession(t *testing.T) {
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-finished")
	if _, err := sessionRepo.End(context.Background(), "session-finished", time.Now()); err != nil {
		t.Fatalf("finish session: %v", err)
	}

	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		telemetry.NewFrameStore(100),
		tracks.OfficialGT7SeedCatalog(),
		events.NewStore(100, events.DedupOptions{}),
		sessionRepo,
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-finished/frames", strings.NewReader(`{"frames":[{"timestampUnixMs":1,"speedMps":50,"rpm":5000,"gear":3,"throttle":0.5,"brake":0,"steering":0,"fuelLiters":30,"positionX":0,"positionY":0,"positionZ":0,"lapNumber":1,"currentLapMs":50000,"isOnTrack":true}]}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "session_finished") {
		t.Fatalf("expected session_finished, got %s", recorder.Body.String())
	}
}

func TestDetectTrackRejectsNonexistentSession(t *testing.T) {
	sessionRepo := sessions.NewMemoryRepository()
	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		telemetry.NewFrameStore(100),
		tracks.OfficialGT7SeedCatalog(),
		events.NewStore(100, events.DedupOptions{}),
		sessionRepo,
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session_nonexistent/track", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "session_not_found") {
		t.Fatalf("expected session_not_found, got %s", recorder.Body.String())
	}
}

func TestDetectTrackAllowsFinishedSession(t *testing.T) {
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-finished")
	if _, err := sessionRepo.End(context.Background(), "session-finished", time.Now()); err != nil {
		t.Fatalf("finish session: %v", err)
	}

	store := telemetry.NewFrameStore(40)
	if err := store.Append(context.Background(), "session-finished", apiStraightCompletedLapFrames(5423, 32)); err != nil {
		t.Fatalf("append frames: %v", err)
	}

	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		tracks.OfficialGT7SeedCatalog(),
		events.NewStore(100, events.DedupOptions{}),
		sessionRepo,
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-finished/track", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d for finished session track, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var result tracks.DetectionResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode detection result: %v", err)
	}
	if result.Status != tracks.DetectionStatusDetected {
		t.Fatalf("expected detected status for finished session, got %q", result.Status)
	}
}

func TestListEventsRejectsNonexistentSession(t *testing.T) {
	sessionRepo := sessions.NewMemoryRepository()
	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		telemetry.NewFrameStore(100),
		tracks.OfficialGT7SeedCatalog(),
		events.NewStore(100, events.DedupOptions{}),
		sessionRepo,
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session_nonexistent/events", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "session_not_found") {
		t.Fatalf("expected session_not_found, got %s", recorder.Body.String())
	}
}

func TestListEventsAllowsFinishedSession(t *testing.T) {
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-finished")
	if _, err := sessionRepo.End(context.Background(), "session-finished", time.Now()); err != nil {
		t.Fatalf("finish session: %v", err)
	}

	eventStore := events.NewStore(100, events.DedupOptions{})
	seedAPIEvent(t, eventStore, apiEngineerEvent(func(event *events.EngineerEvent) {
		event.SessionID = "session-finished"
		event.EventID = "event-finished-1"
	}))

	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		telemetry.NewFrameStore(100),
		tracks.OfficialGT7SeedCatalog(),
		eventStore,
		sessionRepo,
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-finished/events", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d for finished session events, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"eventId":"event-finished-1"`) {
		t.Fatalf("expected event-finished-1 in response, got %s", recorder.Body.String())
	}
}

func TestAnalyzeRejectsNonexistentSession(t *testing.T) {
	aiSvc := &testAIService{resp: ai.GatewayResponse{Summary: "should not be called"}}
	handler := testAnalyzeHandler(t, aiSvc)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session_nonexistent/analyze", strings.NewReader(validAnalyzePayload))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "session_not_found") {
		t.Fatalf("expected session_not_found, got %s", recorder.Body.String())
	}
}

func TestAnalyzeAllowsFinishedSession(t *testing.T) {
	aiSvc := &testAIService{
		resp: ai.GatewayResponse{
			Summary: "Finished session analyzed",
			EventExplanations: []ai.EventExplanation{
				{EventID: "event-1", Type: events.TypeLateThrottle, Explanation: "late throttle exit", Relevance: 0.85},
			},
			Recommendations:  []string{"work on earlier throttle"},
			ReferencedEvents: []string{"event-1"},
			ProviderInfo: ai.ProviderResultInfo{
				Model: "test-model", FinishReason: "stop",
				Usage: ai.UsageInfo{PromptTokens: 50, CompletionTokens: 100, TotalTokens: 150},
			},
			Status: "success",
		},
	}

	cfg := config.Config{Addr: ":0", Env: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	eventStore := events.NewStore(100, events.DedupOptions{})
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-finished")
	if _, err := sessionRepo.End(context.Background(), "session-finished", time.Now()); err != nil {
		t.Fatalf("finish session: %v", err)
	}

	statsRepo := admin.NewMemoryStatsRepo(sessionRepo, frameStore)
	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, frameStore, eventStore)
	handler := routesWithAIAndSessions(cfg, logger, frameStore, catalog, eventStore, sessionRepo, aiSvc, statsRepo, summaryRepo)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-finished/analyze", strings.NewReader(validAnalyzePayload))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d for finished session analyze, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	var response ai.GatewayResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Summary != "Finished session analyzed" {
		t.Fatalf("expected summary on finished session analyze, got %q", response.Summary)
	}
}

func TestHealthEndpointRejectsWrongMethod(t *testing.T) {
	handler := routes(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(method, "/api/v1/health", nil)

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected status %d for %s, got %d", http.StatusMethodNotAllowed, method, recorder.Code)
			}
		})
	}
}
