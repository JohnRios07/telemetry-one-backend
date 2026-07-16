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

type contractSummaryRepo struct {
	summary *sessions.SessionDetailSummary
}

func (r contractSummaryRepo) List(context.Context, sessions.SummaryFilter) (*sessions.ListResponse, error) {
	return &sessions.ListResponse{Sessions: []sessions.SessionSummaryItem{}}, nil
}

func (r contractSummaryRepo) Summary(context.Context, string) (*sessions.SessionDetailSummary, error) {
	return r.summary, nil
}

type recordingSummaryRepo struct {
	limit int
}

func (r *recordingSummaryRepo) List(_ context.Context, filter sessions.SummaryFilter) (*sessions.ListResponse, error) {
	r.limit = filter.Limit
	return &sessions.ListResponse{Sessions: []sessions.SessionSummaryItem{}}, nil
}

func (r *recordingSummaryRepo) Summary(context.Context, string) (*sessions.SessionDetailSummary, error) {
	return &sessions.SessionDetailSummary{}, nil
}

func TestListSessionsContractUsesDefaultLimitWhenOmitted(t *testing.T) {
	repo := &recordingSummaryRepo{}
	handler := listSessionsHandler(repo)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if repo.limit != sessions.DefaultListLimit {
		t.Fatalf("expected default limit %d, got %d", sessions.DefaultListLimit, repo.limit)
	}
}

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

	t.Run("limit=100 accepted as upper bound", func(t *testing.T) {
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

	t.Run("limit=101 rejected", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions?limit=101", nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), "bad_request") || !strings.Contains(recorder.Body.String(), "limit must be between 1 and 100") {
			t.Fatalf("expected bad_request envelope for out-of-range limit, got %s", recorder.Body.String())
		}
	})

	t.Run("limit=0 rejected", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions?limit=0", nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), "bad_request") || !strings.Contains(recorder.Body.String(), "limit must be between 1 and 100") {
			t.Fatalf("expected bad_request envelope for low limit, got %s", recorder.Body.String())
		}
	})

	t.Run("limit=abc rejected", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions?limit=abc", nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), "bad_request") || !strings.Contains(recorder.Body.String(), "limit must be an integer") {
			t.Fatalf("expected bad_request envelope for non-integer limit, got %s", recorder.Body.String())
		}
	})

	t.Run("limit empty rejected", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions?limit=", nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), "bad_request") || !strings.Contains(recorder.Body.String(), "limit must be an integer") {
			t.Fatalf("expected bad_request envelope for empty limit, got %s", recorder.Body.String())
		}
	})
}

func TestSessionSummaryContractFound(t *testing.T) {
	startedAt := time.UnixMilli(1720656000000).UTC()
	handler := routesWithAIAndSessions(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		telemetry.NewFrameStore(10),
		tracks.OfficialGT7SeedCatalog(),
		events.NewStore(10, events.DedupOptions{}),
		sessions.NewMemoryRepository(),
		nil,
		nil,
		contractSummaryRepo{summary: &sessions.SessionDetailSummary{
			Session: sessions.SessionSummaryItem{
				ID:              "session-summary-1",
				Source:          "flutter",
				Game:            "gt7",
				Platform:        "ps5",
				DriverAlias:     "alex",
				TrackID:         "gt7_watkins_glen_international",
				Status:          sessions.StatusActive,
				StartedAt:       startedAt.Format(time.RFC3339),
				FrameBatches:    3,
				PersistedFrames: 12,
				EventCount:      1,
			},
			FrameBatches:       3,
			PersistedFrames:    12,
			LapsDetected:       2,
			EngineerEventCount: 1,
			AIAuditLogCount:    0,
			TimeRangeMs:        &sessions.TimeRange{From: 1000, To: 3000},
		}},
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
	if resp.FrameBatches != 3 {
		t.Fatalf("expected 3 frame batches, got %d", resp.FrameBatches)
	}
	if resp.PersistedFrames != 12 {
		t.Fatalf("expected 12 persisted frames, got %d", resp.PersistedFrames)
	}
	if resp.LapsDetected != 2 {
		t.Fatalf("expected 2 laps detected, got %d", resp.LapsDetected)
	}
	if resp.EngineerEventCount != 1 {
		t.Fatalf("expected 1 engineer event, got %d", resp.EngineerEventCount)
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
	if resp.Session.FrameBatches != 3 {
		t.Fatalf("expected nested frameBatches 3, got %d", resp.Session.FrameBatches)
	}
	if resp.Session.PersistedFrames != 12 {
		t.Fatalf("expected nested persistedFrames 12, got %d", resp.Session.PersistedFrames)
	}
	if resp.Session.EventCount != 1 {
		t.Fatalf("expected nested eventCount 1, got %d", resp.Session.EventCount)
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
