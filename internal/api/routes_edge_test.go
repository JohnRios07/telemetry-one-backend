package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"telemetry-one-backend/internal/config"
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
