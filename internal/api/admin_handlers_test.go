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

	"telemetry-one-backend/internal/admin"
	"telemetry-one-backend/internal/ai"
	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

func TestAdminIngestStats_OpenWhenNoToken(t *testing.T) {
	handler := newAdminTestHandler(t, "", false)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingest-stats", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var resp admin.IngestStatsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}
	if resp.Mode != admin.ModeMemory {
		t.Fatalf("expected mode %q, got %q", admin.ModeMemory, resp.Mode)
	}
}

func TestAdminIngestStats_UnauthorizedWhenTokenMissing(t *testing.T) {
	handler := newAdminTestHandler(t, "secret-token", false)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingest-stats", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"unauthorized"`) {
		t.Fatalf("expected unauthorized code, got %s", recorder.Body.String())
	}
}

func TestAdminIngestStats_UnauthorizedWhenBadToken(t *testing.T) {
	handler := newAdminTestHandler(t, "secret-token", false)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingest-stats", nil)
	request.Header.Set("Authorization", "Bearer wrong-token")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"unauthorized"`) {
		t.Fatalf("expected unauthorized code, got %s", recorder.Body.String())
	}
}

func TestAdminIngestStats_UnauthorizedWhenMalformedHeader(t *testing.T) {
	handler := newAdminTestHandler(t, "secret-token", false)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingest-stats", nil)
	request.Header.Set("Authorization", "Basic not-bearer")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

func TestAdminIngestStats_AuthorizedWithCorrectToken(t *testing.T) {
	handler := newAdminTestHandler(t, "secret-token", true)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingest-stats", nil)
	request.Header.Set("Authorization", "Bearer secret-token")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
}

func TestAdminIngestStats_ReturnsSeededData(t *testing.T) {
	handler := newAdminTestHandler(t, "", true)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingest-stats", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var resp admin.IngestStatsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}
	if resp.Totals.Sessions != 1 {
		t.Fatalf("expected 1 session, got %d", resp.Totals.Sessions)
	}
	if resp.Totals.ActiveSessions != 1 {
		t.Fatalf("expected 1 active session, got %d", resp.Totals.ActiveSessions)
	}
}

func TestAdminIngestStats_RejectsInvalidQueryParams(t *testing.T) {
	handler := newAdminTestHandler(t, "", true)

	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "limit too low", url: "/api/v1/admin/ingest-stats?limit=0", want: "limit must be between 1 and 100"},
		{name: "limit too high", url: "/api/v1/admin/ingest-stats?limit=101", want: "limit must be between 1 and 100"},
		{name: "limit not integer", url: "/api/v1/admin/ingest-stats?limit=abc", want: "limit must be an integer"},
		{name: "days too low", url: "/api/v1/admin/ingest-stats?days=0", want: "days must be between 1 and 90"},
		{name: "days too high", url: "/api/v1/admin/ingest-stats?days=91", want: "days must be between 1 and 90"},
		{name: "days not integer", url: "/api/v1/admin/ingest-stats?days=abc", want: "days must be an integer"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.url, nil))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), "bad_request") || !strings.Contains(recorder.Body.String(), tc.want) {
				t.Fatalf("expected bad_request envelope containing %q, got %s", tc.want, recorder.Body.String())
			}
		})
	}
}

func newAdminTestHandler(t *testing.T, adminToken string, seedSession bool) http.Handler {
	t.Helper()
	cfg := config.Config{Addr: ":0", Env: "test", AdminToken: adminToken}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	eventStore := events.NewStore(100, events.DedupOptions{})
	sessionRepo := sessions.NewMemoryRepository()

	if seedSession {
		seedTestSession(t, sessionRepo, "session-admin-1")
	}

	statsRepo := admin.NewMemoryStatsRepo(sessionRepo, frameStore)
	aiSvc := &noopAIService{}
	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, frameStore, eventStore)
	return routesWithAIAndSessions(cfg, logger, frameStore, catalog, eventStore, sessionRepo, aiSvc, statsRepo, summaryRepo)
}

type noopAIService struct{}

func (n *noopAIService) Analyze(_ context.Context, _ ai.GatewayRequest) (ai.GatewayResponse, error) {
	return ai.GatewayResponse{}, nil
}
