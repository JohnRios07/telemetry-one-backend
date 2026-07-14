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

func TestListSessionsContractSeededSingle(t *testing.T) {
	handler := newTestHandler(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var resp sessions.ListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Sessions == nil {
		t.Fatal("expected non-nil sessions array, got nil")
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("expected 1 session (test handler seeds one), got %d", len(resp.Sessions))
	}
}

func TestListSessionsContractSeeded(t *testing.T) {
	store := telemetry.NewFrameStore(10)
	eventStore := events.NewStore(10, events.DedupOptions{})
	sessionRepo := sessions.NewMemoryRepository()
	startedAt := time.UnixMilli(1720656000000).UTC()
	if _, err := sessionRepo.Create(context.Background(), sessions.Session{ID: "session-a", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: startedAt}); err != nil {
		t.Fatalf("seed session a: %v", err)
	}
	if _, err := sessionRepo.Create(context.Background(), sessions.Session{ID: "session-b", Source: "unity", Game: "gt7", Platform: "ps5", DriverAlias: "driver-b", StartedAt: startedAt.Add(time.Second)}); err != nil {
		t.Fatalf("seed session b: %v", err)
	}

	if err := store.Append(context.Background(), "session-a", []telemetry.Frame{{TimestampUnixMs: 1}, {TimestampUnixMs: 2}}); err != nil {
		t.Fatalf("append frames: %v", err)
	}

	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		tracks.OfficialGT7SeedCatalog(),
		eventStore,
		sessionRepo,
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var resp sessions.ListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(resp.Sessions))
	}

	if resp.Sessions[0].ID != "session-b" || resp.Sessions[1].ID != "session-a" {
		t.Fatalf("expected session-b first (most recent), got %s then %s", resp.Sessions[0].ID, resp.Sessions[1].ID)
	}

	if resp.Sessions[1].PersistedFrames != 2 {
		t.Fatalf("expected session-a to have 2 persisted frames, got %d", resp.Sessions[1].PersistedFrames)
	}
	if resp.Sessions[0].DriverAlias != "driver-b" {
		t.Fatalf("expected session-b driverAlias, got %q", resp.Sessions[0].DriverAlias)
	}
}

func TestListSessionsContractLimit(t *testing.T) {
	store := telemetry.NewFrameStore(10)
	eventStore := events.NewStore(10, events.DedupOptions{})
	sessionRepo := sessions.NewMemoryRepository()
	startedAt := time.UnixMilli(1720656000000).UTC()
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		if _, err := sessionRepo.Create(context.Background(), sessions.Session{ID: "session-" + id, Source: "test", Game: "gt7", Platform: "ps5", StartedAt: startedAt.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatalf("seed session: %v", err)
		}
	}

	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		tracks.OfficialGT7SeedCatalog(),
		eventStore,
		sessionRepo,
	)

	t.Run("limit=2", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions?limit=2", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
		}
		var resp sessions.ListResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Sessions) != 2 {
			t.Fatalf("expected 2 sessions with limit=2, got %d", len(resp.Sessions))
		}
	})

	t.Run("limit=100 (clamped)", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions?limit=100", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
		}
		var resp sessions.ListResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Sessions) != 5 {
			t.Fatalf("expected all 5 sessions with limit=100, got %d", len(resp.Sessions))
		}
	})

	t.Run("limit=0 clamped to min", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions?limit=0", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
		}
		var resp sessions.ListResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Sessions) != 1 {
			t.Fatalf("expected 1 session (clamped to min), got %d", len(resp.Sessions))
		}
	})
}

func TestSessionSummaryContractFound(t *testing.T) {
	store := telemetry.NewFrameStore(10)
	eventStore := events.NewStore(10, events.DedupOptions{})
	sessionRepo := sessions.NewMemoryRepository()
	startedAt := time.UnixMilli(1720656000000).UTC()
	if _, err := sessionRepo.Create(context.Background(), sessions.Session{
		ID: "session-summary-1", Source: "flutter", Game: "gt7", Platform: "ps5",
		DriverAlias: "alex", TrackID: "gt7_watkins_glen_international", StartedAt: startedAt,
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if err := store.Append(context.Background(), "session-summary-1", []telemetry.Frame{
		{TimestampUnixMs: 1000, LapNumber: 1},
		{TimestampUnixMs: 2000, LapNumber: 1},
		{TimestampUnixMs: 3000, LapNumber: 2},
	}); err != nil {
		t.Fatalf("append frames: %v", err)
	}

	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		tracks.OfficialGT7SeedCatalog(),
		eventStore,
		sessionRepo,
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-summary-1/summary", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var resp sessions.SessionDetailSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Session.ID != "session-summary-1" || resp.Session.Source != "flutter" {
		t.Fatalf("unexpected session metadata: %+v", resp.Session)
	}
	if resp.PersistedFrames != 3 {
		t.Fatalf("expected 3 persisted frames, got %d", resp.PersistedFrames)
	}
	if resp.LapsDetected != 2 {
		t.Fatalf("expected 2 laps detected, got %d", resp.LapsDetected)
	}
	if resp.TimeRangeMs == nil || resp.TimeRangeMs.From != 1000 || resp.TimeRangeMs.To != 3000 {
		t.Fatalf("expected timeRange [1000, 3000], got %+v", resp.TimeRangeMs)
	}
	if resp.Session.DriverAlias != "alex" {
		t.Fatalf("expected driverAlias, got %q", resp.Session.DriverAlias)
	}
	if resp.Session.TrackID != "gt7_watkins_glen_international" {
		t.Fatalf("expected trackId, got %q", resp.Session.TrackID)
	}
}

func TestSessionSummaryContractNotFound(t *testing.T) {
	handler := newTestHandler(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-nonexistent/summary", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "session_not_found") {
		t.Fatalf("expected session_not_found error, got %s", recorder.Body.String())
	}
}
