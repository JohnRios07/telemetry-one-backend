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

	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

func TestSessionExportDisabledByDefault(t *testing.T) {
	_, frameStore := finishedExportSession(t, "session-1", exportFrames(1, 2), true)
	handler := sessionExportTestHandler(t, config.Config{Addr: ":0", Env: "test"}, sessions.NewMemoryRepository(), frameStore)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/export/trackbuilder", nil))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusForbidden, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"error"`) || !strings.Contains(recorder.Body.String(), `"forbidden"`) {
		t.Fatalf("expected forbidden API error, got %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"apiVersion"`) {
		t.Fatalf("expected unversioned error envelope, got %s", recorder.Body.String())
	}
}

func TestSessionExportReturnsRawJSONAndHeaders(t *testing.T) {
	sessionRepo, frameStore := finishedExportSession(t, "session-1", exportFrames(10, 20), true)
	handler := sessionExportTestHandler(t, config.Config{Addr: ":0", Env: "test", EnableSessionExport: true}, sessionRepo, frameStore)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/export/trackbuilder", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected content type application/json, got %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `attachment; filename="session-session-1-trackbuilder.json"` {
		t.Fatalf("unexpected content disposition: %q", got)
	}
	if strings.Contains(recorder.Body.String(), `"apiVersion"`) || strings.Contains(recorder.Body.String(), `"error"`) || strings.Contains(recorder.Body.String(), `"session":`) {
		t.Fatalf("expected raw ingest payload, got %s", recorder.Body.String())
	}

	var payload telemetry.IngestBatchRequest
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("expected raw ingest JSON: %v", err)
	}
	if payload.SessionID != "session-1" || len(payload.Frames) != 2 || payload.Frames[0].TimestampUnixMs != 10 || payload.Frames[1].TimestampUnixMs != 20 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestSessionExportFiltersLapNumber(t *testing.T) {
	frames := []telemetry.Frame{exportFrameAt(10, 2), exportFrameAt(20, 3), exportFrameAt(30, 3), exportFrameAt(40, 4)}
	sessionRepo, frameStore := finishedExportSession(t, "session-1", frames, true)
	handler := sessionExportTestHandler(t, config.Config{Addr: ":0", Env: "test", EnableSessionExport: true}, sessionRepo, frameStore)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/export/trackbuilder?lapNumber=3", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	var payload telemetry.IngestBatchRequest
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("expected raw ingest JSON: %v", err)
	}
	if got := len(payload.Frames); got != 2 {
		t.Fatalf("expected 2 filtered frames, got %d", got)
	}
	if payload.Frames[0].LapNumber != 3 || payload.Frames[1].LapNumber != 3 {
		t.Fatalf("expected only lap 3 frames, got %#v", payload.Frames)
	}
	if payload.Frames[0].TimestampUnixMs != 20 || payload.Frames[1].TimestampUnixMs != 30 {
		t.Fatalf("expected lap order preserved, got %#v", payload.Frames)
	}
}

func TestSessionExportRejectsMissingSession(t *testing.T) {
	handler := sessionExportTestHandler(t, config.Config{Addr: ":0", Env: "test", EnableSessionExport: true}, sessions.NewMemoryRepository(), telemetry.NewFrameStore(100))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/missing/export/trackbuilder", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "session_not_found") {
		t.Fatalf("expected session_not_found error, got %s", recorder.Body.String())
	}
}

func TestSessionExportRejectsActiveSession(t *testing.T) {
	sessionRepo, frameStore := activeExportSession(t, "session-1")
	handler := sessionExportTestHandler(t, config.Config{Addr: ":0", Env: "test", EnableSessionExport: true}, sessionRepo, frameStore)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/export/trackbuilder", nil))

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "session_export_blocked") {
		t.Fatalf("expected active-session rejection, got %s", recorder.Body.String())
	}
}

func TestSessionExportRejectsEmptyFrames(t *testing.T) {
	sessionRepo, frameStore := finishedExportSession(t, "session-1", nil, true)
	handler := sessionExportTestHandler(t, config.Config{Addr: ":0", Env: "test", EnableSessionExport: true}, sessionRepo, frameStore)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/export/trackbuilder", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "no persisted frames found") {
		t.Fatalf("expected empty-frame rejection, got %s", recorder.Body.String())
	}
}

func TestSessionExportRejectsNegativeLapNumber(t *testing.T) {
	sessionRepo, frameStore := finishedExportSession(t, "session-1", exportFrames(10, 20), true)
	handler := sessionExportTestHandler(t, config.Config{Addr: ":0", Env: "test", EnableSessionExport: true}, sessionRepo, frameStore)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/export/trackbuilder?lapNumber=-1", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "lapNumber must be zero or greater") {
		t.Fatalf("expected invalid lap number rejection, got %s", recorder.Body.String())
	}
}

func TestSessionExportRejectsNonMonotonicFrames(t *testing.T) {
	sessionRepo, frameStore := finishedExportSession(t, "session-1", []telemetry.Frame{exportFrameAt(10, 1), exportFrameAt(10, 1)}, true)
	handler := sessionExportTestHandler(t, config.Config{Addr: ":0", Env: "test", EnableSessionExport: true}, sessionRepo, frameStore)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/export/trackbuilder", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "exported timestamps must strictly increase") {
		t.Fatalf("expected monotonicity rejection, got %s", recorder.Body.String())
	}
}

func sessionExportTestHandler(t *testing.T, cfg config.Config, sessionRepo sessions.Repository, frameStore telemetry.Store) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return routesWithSessionRepository(cfg, logger, frameStore, tracks.OfficialGT7SeedCatalog(), events.NewStore(100, events.DedupOptions{}), sessionRepo)
}

func finishedExportSession(t *testing.T, id string, frames []telemetry.Frame, finished bool) (sessions.Repository, telemetry.Store) {
	t.Helper()
	repo := sessions.NewMemoryRepository()
	store := telemetry.NewFrameStore(100)
	startedAt := time.UnixMilli(1720656000000).UTC()
	created, err := repo.Create(context.Background(), sessions.Session{ID: id, Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: startedAt})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if len(frames) > 0 {
		if err := store.Append(context.Background(), created.ID, frames); err != nil {
			t.Fatalf("seed frames: %v", err)
		}
	}
	if finished {
		if _, err := repo.End(context.Background(), created.ID, time.UnixMilli(1720656123456).UTC()); err != nil {
			t.Fatalf("finish session: %v", err)
		}
	}
	return repo, store
}

func activeExportSession(t *testing.T, id string) (sessions.Repository, telemetry.Store) {
	t.Helper()
	repo := sessions.NewMemoryRepository()
	store := telemetry.NewFrameStore(100)
	startedAt := time.UnixMilli(1720656000000).UTC()
	if _, err := repo.Create(context.Background(), sessions.Session{ID: id, Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: startedAt}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return repo, store
}

func exportFrames(timestamps ...int64) []telemetry.Frame {
	frames := make([]telemetry.Frame, 0, len(timestamps))
	for _, timestamp := range timestamps {
		frames = append(frames, exportFrame(timestamp))
	}
	return frames
}

func exportFrame(timestamp int64) telemetry.Frame { return exportFrameAt(timestamp, 1) }

func exportFrameAt(timestamp int64, lapNumber int) telemetry.Frame {
	return telemetry.Frame{TimestampUnixMs: timestamp, SpeedMps: 58.33, RPM: 7100, Gear: 4, Throttle: 0.7, Brake: 0.2, Steering: -0.12, FuelLiters: 38.4, PositionX: 123.4, PositionY: 5.6, PositionZ: 789.1, LapNumber: lapNumber, CurrentLapMs: 81234, IsOnTrack: true}
}
