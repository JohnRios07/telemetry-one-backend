package api

import (
	"bytes"
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
	"time"

	"telemetry-one-backend/internal/admin"
	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/laps"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details struct {
			RejectionCode string `json:"rejectionCode"`
			Category      string `json:"category"`
			Field         string `json:"field"`
			FrameIndex    *int   `json:"frameIndex"`
			MaxFrames     int    `json:"maxFrames"`
		} `json:"details"`
	} `json:"error"`
}

func TestGetSessionReturnsErrorWhenEventCountCannotBeLoaded(t *testing.T) {
	store := telemetry.NewFrameStore(10)
	sessionRepo := sessions.NewMemoryRepository()
	startedAt := time.UnixMilli(1720656000000).UTC()
	if _, err := sessionRepo.Create(context.Background(), sessions.Session{ID: "session-1", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: startedAt}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		tracks.OfficialGT7SeedCatalog(),
		failingEventStore{},
		sessionRepo,
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "event repository error") {
		t.Fatalf("expected event repository error, got %s", recorder.Body.String())
	}
}

func TestGetSessionReturnsFrameRepositoryErrorWhenFrameCountCannotBeLoaded(t *testing.T) {
	sessionRepo := sessions.NewMemoryRepository()
	startedAt := time.UnixMilli(1720656000000).UTC()
	if _, err := sessionRepo.Create(context.Background(), sessions.Session{ID: "session-1", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: startedAt}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		failingFrameStore{err: errors.New("frames failed")},
		tracks.OfficialGT7SeedCatalog(),
		events.NewStore(10, events.DedupOptions{}),
		sessionRepo,
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "frame repository error") {
		t.Fatalf("expected frame repository error, got %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "event repository error") {
		t.Fatalf("expected frame error not event error, got %s", recorder.Body.String())
	}
}

func TestCreateSessionContractValidatesShape(t *testing.T) {
	handler := routes(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"source":"flutter","game":"gt7","platform":"ps5","startedUnixMs":1720656000000}`

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(body))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"id":"session_`) || !strings.Contains(recorder.Body.String(), `"status":"active"`) || !strings.Contains(recorder.Body.String(), `"endedAt":null`) {
		t.Fatalf("expected active session response with generated id, got %s", recorder.Body.String())
	}
}

func TestV2APIVersionMarkerIsAddedToSafeAPIv1SuccessObjects(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantField  string
	}{
		{name: "health", method: http.MethodGet, path: "/api/v1/health", wantStatus: http.StatusOK, wantField: "status"},
		{name: "create session", method: http.MethodPost, path: "/api/v1/sessions", body: `{"source":"flutter","game":"gt7","platform":"ps5","startedUnixMs":1720656000000}`, wantStatus: http.StatusCreated, wantField: "session"},
		{name: "get session", method: http.MethodGet, path: "/api/v1/sessions/session-1", wantStatus: http.StatusOK, wantField: "session"},
		{name: "ingest frames", method: http.MethodPost, path: "/api/v1/sessions/session-1/frames", body: `{"sessionId":"session-1","frames":[{"timestampUnixMs":1720656000000,"speedMps":58.33,"rpm":7100,"gear":4,"throttle":0.82,"brake":0,"steering":-0.12,"fuelLiters":38.4,"positionX":123.4,"positionY":5.6,"positionZ":789.1,"lapNumber":2,"currentLapMs":81234,"isOnTrack":true}]}`, wantStatus: http.StatusAccepted, wantField: "acceptedFrames"},
		{name: "track detection", method: http.MethodGet, path: "/api/v1/sessions/session-1/track", wantStatus: http.StatusOK, wantField: "status"},
		{name: "events", method: http.MethodGet, path: "/api/v1/sessions/session-1/events", wantStatus: http.StatusOK, wantField: "events"},
		{name: "session list", method: http.MethodGet, path: "/api/v1/sessions", wantStatus: http.StatusOK, wantField: "sessions"},
		{name: "session summary", method: http.MethodGet, path: "/api/v1/sessions/session-1/summary", wantStatus: http.StatusOK, wantField: "session"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			handler.ServeHTTP(recorder, request)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d with body %s", tc.wantStatus, recorder.Code, recorder.Body.String())
			}
			response := decodeJSONMap(t, recorder.Body.Bytes())
			assertAPIVersion(t, response)
			if _, ok := response[tc.wantField]; !ok {
				t.Fatalf("expected existing field %q to remain present in %+v", tc.wantField, response)
			}
		})
	}
}

func TestV2APIVersionMarkerPreservesErrorAndNonObjectShapes(t *testing.T) {
	handler := newTestHandler(t)

	errorRecorder := httptest.NewRecorder()
	handler.ServeHTTP(errorRecorder, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(`{"sessionId":"session-1","frames":[]}`)))
	if errorRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, errorRecorder.Code, errorRecorder.Body.String())
	}
	errorResponse := decodeJSONMap(t, errorRecorder.Body.Bytes())
	if _, ok := errorResponse["apiVersion"]; ok {
		t.Fatalf("expected error envelope to remain unversioned, got %+v", errorResponse)
	}
	if _, ok := errorResponse["error"]; !ok {
		t.Fatalf("expected existing error envelope shape, got %+v", errorResponse)
	}

	nonObject := payloadWithAPIVersion(http.StatusOK, []string{"ok"})
	if _, ok := nonObject.([]string); !ok {
		t.Fatalf("expected non-object payload to remain unchanged, got %#v", nonObject)
	}
}

func TestAPIVersionDoesNotRequireAPIV2Routes(t *testing.T) {
	handler := routes(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v2/health", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected /api/v2 route to remain absent with status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestIngestPersistsCompletedLapsIdempotently(t *testing.T) {
	store := telemetry.NewFrameStore(100)
	eventStore := events.NewStore(100, events.DedupOptions{})
	lapRepo := laps.NewMemoryRepository()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-1")
	handler := routesWithLapRepository(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)), store, tracks.OfficialGT7SeedCatalog(), eventStore, lapRepo, sessionRepo)

	lastLapMs := int64(91234)
	bestLapMs := int64(89000)
	body, err := json.Marshal(telemetry.IngestBatchRequest{SessionID: "session-1", Frames: []telemetry.Frame{{
		TimestampUnixMs: 1720656000000,
		SpeedMps:        58.33,
		RPM:             7100,
		Gear:            4,
		Throttle:        0.82,
		Brake:           0,
		Steering:        -0.12,
		FuelLiters:      38.4,
		PositionX:       123.4,
		PositionY:       5.6,
		PositionZ:       789.1,
		LapNumber:       3,
		CurrentLapMs:    123,
		LastLapMs:       &lastLapMs,
		BestLapMs:       &bestLapMs,
		IsOnTrack:       true,
	}}})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", bytes.NewReader(body)))
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("expected ingest %d to return %d, got %d body %s", i+1, http.StatusAccepted, recorder.Code, recorder.Body.String())
		}
	}

	got, err := lapRepo.ListBySession(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("list completed laps: %v", err)
	}
	if len(got) != 1 || got[0].LapNumber != 2 || got[0].LapTimeMs != lastLapMs {
		t.Fatalf("expected one completed lap 2, got %+v", got)
	}
}

func TestIngestPersistsCompletedLapsWithZeroBestLapMs(t *testing.T) {
	store := telemetry.NewFrameStore(100)
	eventStore := events.NewStore(100, events.DedupOptions{})
	lapRepo := laps.NewMemoryRepository()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-zero-best-lap")
	handler := routesWithLapRepository(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)), store, tracks.OfficialGT7SeedCatalog(), eventStore, lapRepo, sessionRepo)

	lastLapMs := int64(91234)
	bestLapMs := int64(0)
	body, err := json.Marshal(telemetry.IngestBatchRequest{SessionID: "session-zero-best-lap", Frames: []telemetry.Frame{{
		TimestampUnixMs: 1720656000000,
		SpeedMps:        58.33,
		RPM:             7100,
		Gear:            4,
		Throttle:        0.82,
		Brake:           0,
		Steering:        -0.12,
		FuelLiters:      38.4,
		PositionX:       123.4,
		PositionY:       5.6,
		PositionZ:       789.1,
		LapNumber:       3,
		CurrentLapMs:    123,
		LastLapMs:       &lastLapMs,
		BestLapMs:       &bestLapMs,
		IsOnTrack:       true,
	}}})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-zero-best-lap/frames", bytes.NewReader(body)))
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("expected ingest %d to return %d, got %d body %s", i+1, http.StatusAccepted, recorder.Code, recorder.Body.String())
		}
	}

	got, err := lapRepo.ListBySession(context.Background(), "session-zero-best-lap")
	if err != nil {
		t.Fatalf("list completed laps: %v", err)
	}
	if len(got) != 1 || got[0].LapNumber != 2 || got[0].LapTimeMs != lastLapMs || got[0].BestLapMs == nil || *got[0].BestLapMs != 0 {
		t.Fatalf("expected one completed lap 2 with zero best lap ms, got %+v", got)
	}
}

func TestIngestPersistsTelemetryGapCountAcrossBatchesIdempotently(t *testing.T) {
	store := telemetry.NewFrameStore(100)
	eventStore := events.NewStore(100, events.DedupOptions{})
	lapRepo := laps.NewMemoryRepository()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-gaps")
	handler := routesWithLapRepository(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)), store, tracks.OfficialGT7SeedCatalog(), eventStore, lapRepo, sessionRepo)

	ingestFrames(t, handler, "session-gaps", []telemetry.Frame{
		completedLapFrame(1720656000000, 1, nil, nil),
		completedLapFrame(1720656000050, 1, nil, nil),
		completedLapFrame(1720656000100, 1, nil, nil),
		completedLapFrame(1720656000150, 1, nil, nil),
	})
	lastLapMs := int64(91234)
	secondBatch := []telemetry.Frame{
		completedLapFrame(1720656000700, 1, nil, nil),
		completedLapFrame(1720656000750, 1, nil, nil),
		{TimestampUnixMs: 1720656000800, SpeedMps: 58.33, RPM: 7100, Gear: 4, Throttle: 0.82, Brake: 0, Steering: -0.12, FuelLiters: 38.4,
			PositionX: 123.4, PositionY: 5.6, PositionZ: 789.1, LapNumber: 2, CurrentLapMs: 100, LastLapMs: &lastLapMs, IsOnTrack: true},
	}
	ingestFrames(t, handler, "session-gaps", secondBatch)
	ingestFrames(t, handler, "session-gaps", secondBatch)

	got, err := lapRepo.ListBySession(context.Background(), "session-gaps")
	if err != nil {
		t.Fatalf("list completed laps: %v", err)
	}
	if len(got) != 1 || got[0].LapNumber != 1 || got[0].TelemetryGapCount != 1 {
		t.Fatalf("expected completed lap 1 with one deterministic telemetry gap, got %+v", got)
	}
}

func TestIngestDoesNotPersistRejectedFrameLapEvidence(t *testing.T) {
	store := telemetry.NewFrameStore(100)
	lapRepo := laps.NewMemoryRepository()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-1")
	handler := routesWithLapRepository(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)), store, tracks.OfficialGT7SeedCatalog(), events.NewStore(100, events.DedupOptions{}), lapRepo, sessionRepo)

	body := `{"sessionId":"session-1","frames":[{"timestampUnixMs":1720656000000,"speedMps":58.33,"rpm":7100,"gear":4,"throttle":1.2,"brake":0,"steering":-0.12,"fuelLiters":38.4,"positionX":123.4,"positionY":5.6,"positionZ":789.1,"lapNumber":3,"currentLapMs":123,"lastLapMs":91234,"isOnTrack":true}]}`
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(body)))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected rejected ingest status %d, got %d body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}

	got, err := lapRepo.ListBySession(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("list completed laps: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected rejected frame not to persist laps, got %+v", got)
	}
}

func TestIngestSkipsLapSamplesWhenCompletedLapHasNoDistance(t *testing.T) {
	store := telemetry.NewFrameStore(100)
	lapRepo := laps.NewMemoryRepository()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-no-distance")
	handler := routesWithLapAndSampleRepositories(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)), store, tracks.OfficialGT7SeedCatalog(), events.NewStore(100, events.DedupOptions{}), lapRepo, lapRepo, sessionRepo)

	ingestFrames(t, handler, "session-no-distance", []telemetry.Frame{
		completedLapFrame(1720656000000, 1, nil, nil),
		completedLapFrame(1720656001000, 1, nil, nil),
	})
	lastLapMs := int64(91234)
	ingestFrames(t, handler, "session-no-distance", []telemetry.Frame{{
		TimestampUnixMs: 1720656002000, SpeedMps: 58.33, RPM: 7100, Gear: 4, Throttle: 0.82, Brake: 0, Steering: -0.12, FuelLiters: 38.4,
		PositionX: 123.4, PositionY: 5.6, PositionZ: 789.1, LapNumber: 2, CurrentLapMs: 100, LastLapMs: &lastLapMs, IsOnTrack: true,
	}})

	samples, err := lapRepo.ListSamples(context.Background(), laps.NewCompletedLapID("session-no-distance", 1))
	if err != nil {
		t.Fatalf("list samples: %v", err)
	}
	if len(samples) != 0 {
		t.Fatalf("expected no samples without explicit distance, got %+v", samples)
	}
}

func TestIngestPersistsLapSamplesForCompletedLapWithMonotonicDistance(t *testing.T) {
	store := telemetry.NewFrameStore(100)
	lapRepo := laps.NewMemoryRepository()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-samples")
	handler := routesWithLapAndSampleRepositories(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)), store, tracks.OfficialGT7SeedCatalog(), events.NewStore(100, events.DedupOptions{}), lapRepo, lapRepo, sessionRepo)

	ingestFrames(t, handler, "session-samples", []telemetry.Frame{
		completedLapFrame(1720656000000, 1, apiFloat64Ptr(0), apiFloat64Ptr(0)),
		completedLapFrame(1720656001000, 1, apiFloat64Ptr(10), nil),
	})
	lastLapMs := int64(91234)
	ingestFrames(t, handler, "session-samples", []telemetry.Frame{{
		TimestampUnixMs: 1720656002000, SpeedMps: 58.33, RPM: 7100, Gear: 4, Throttle: 0.82, Brake: 0, Steering: -0.12, FuelLiters: 38.4,
		PositionX: 123.4, PositionY: 5.6, PositionZ: 789.1, LapNumber: 2, CurrentLapMs: 100, LastLapMs: &lastLapMs, IsOnTrack: true,
	}})

	samples, err := lapRepo.ListSamples(context.Background(), laps.NewCompletedLapID("session-samples", 1))
	if err != nil {
		t.Fatalf("list samples: %v", err)
	}
	if len(samples) != 3 {
		t.Fatalf("expected 0,5,10m samples, got %+v", samples)
	}
	if samples[0].DistanceMeters != 0 || samples[1].DistanceMeters != 5 || samples[2].DistanceMeters != 10 {
		t.Fatalf("expected sorted 5m samples, got %+v", samples)
	}
	if samples[0].YawRate == nil || *samples[0].YawRate != 0 {
		t.Fatalf("expected explicit optional zero preserved at exact bucket, got %+v", samples[0].YawRate)
	}
	if samples[1].YawRate != nil {
		t.Fatalf("expected missing optional channel to stay nil during interpolation, got %+v", samples[1].YawRate)
	}
}

func TestIngestDoesNotSampleIncompleteLap(t *testing.T) {
	store := telemetry.NewFrameStore(100)
	lapRepo := laps.NewMemoryRepository()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-incomplete")
	handler := routesWithLapAndSampleRepositories(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)), store, tracks.OfficialGT7SeedCatalog(), events.NewStore(100, events.DedupOptions{}), lapRepo, lapRepo, sessionRepo)

	ingestFrames(t, handler, "session-incomplete", []telemetry.Frame{
		completedLapFrame(1720656000000, 1, apiFloat64Ptr(0), nil),
		completedLapFrame(1720656001000, 1, apiFloat64Ptr(10), nil),
	})

	samples, err := lapRepo.ListSamples(context.Background(), laps.NewCompletedLapID("session-incomplete", 1))
	if err != nil {
		t.Fatalf("list samples: %v", err)
	}
	if len(samples) != 0 {
		t.Fatalf("expected incomplete lap not to sample, got %+v", samples)
	}
}

func ingestFrames(t *testing.T, handler http.Handler, sessionID string, frames []telemetry.Frame) {
	t.Helper()
	body, err := json.Marshal(telemetry.IngestBatchRequest{SessionID: sessionID, Frames: frames})
	if err != nil {
		t.Fatalf("marshal ingest body: %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sessionID+"/frames", bytes.NewReader(body)))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected ingest to return %d, got %d body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}
}

func completedLapFrame(timestamp int64, lapNumber int, distance *float64, yawRate *float64) telemetry.Frame {
	return telemetry.Frame{
		TimestampUnixMs: timestamp, SpeedMps: float64(timestamp-1720655999990) / 100, RPM: 1000, Gear: 3, Throttle: 0.5, Brake: 0, Steering: 0, FuelLiters: 38.4,
		PositionX: 123.4, PositionY: 5.6, PositionZ: 789.1, YawRate: yawRate, LapDistanceMeters: distance, LapNumber: lapNumber, CurrentLapMs: timestamp - 1720656000000, IsOnTrack: true,
	}
}

func apiFloat64Ptr(value float64) *float64 { return &value }

func TestIngestReturnsInternalServerErrorWhenLapPersistenceFails(t *testing.T) {
	store := telemetry.NewFrameStore(100)
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-1")
	handler := routesWithLapRepository(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)), store, tracks.OfficialGT7SeedCatalog(), events.NewStore(100, events.DedupOptions{}), failingLapRepository{err: errors.New("laps failed")}, sessionRepo)

	lastLapMs := int64(91234)
	body, err := json.Marshal(telemetry.IngestBatchRequest{SessionID: "session-1", Frames: []telemetry.Frame{{
		TimestampUnixMs: 1720656000000,
		SpeedMps:        58.33,
		RPM:             7100,
		Gear:            4,
		Throttle:        0.82,
		Brake:           0,
		Steering:        -0.12,
		FuelLiters:      38.4,
		PositionX:       123.4,
		PositionY:       5.6,
		PositionZ:       789.1,
		LapNumber:       3,
		CurrentLapMs:    123,
		LastLapMs:       &lastLapMs,
		IsOnTrack:       true,
	}}})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", bytes.NewReader(body)))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d body %s", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "lap persistence error") {
		t.Fatalf("expected explicit lap persistence error, got %s", recorder.Body.String())
	}
}

func TestSessionLifecycleCreateGetFinish(t *testing.T) {
	store := telemetry.NewFrameStore(10)
	eventStore := events.NewStore(10, events.DedupOptions{})
	sessionRepo := sessions.NewMemoryRepository()
	handler := routesWithSessionRepository(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)), store, tracks.OfficialGT7SeedCatalog(), eventStore, sessionRepo)

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"source":"flutter","game":"gt7","platform":"ps5","driverAlias":"alex","trackId":"gt7_watkins_glen_international","startedUnixMs":1720656000000}`))
	handler.ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d with body %s", http.StatusCreated, createRecorder.Code, createRecorder.Body.String())
	}
	var createResponse sessions.Response
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createResponse); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}
	if createResponse.Session.ID == "" || !strings.HasPrefix(createResponse.Session.ID, "session_") {
		t.Fatalf("expected backend-owned session id, got %+v", createResponse.Session)
	}
	if createResponse.Session.Status != sessions.StatusActive || createResponse.Session.EndedAt != nil || createResponse.Session.StartedAt != time.UnixMilli(1720656000000).UTC() {
		t.Fatalf("unexpected created session: %+v", createResponse.Session)
	}

	if err := store.Append(context.Background(), createResponse.Session.ID, []telemetry.Frame{{TimestampUnixMs: 1720656000000}}); err != nil {
		t.Fatalf("append frames: %v", err)
	}
	seedAPIEvent(t, eventStore, apiEngineerEvent(func(event *events.EngineerEvent) {
		event.EventID = "event-lifecycle-1"
		event.SessionID = createResponse.Session.ID
	}))

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+createResponse.Session.ID, nil)
	handler.ServeHTTP(getRecorder, getRequest)

	if getRecorder.Code != http.StatusOK {
		t.Fatalf("expected get status %d, got %d with body %s", http.StatusOK, getRecorder.Code, getRecorder.Body.String())
	}
	var getResponse sessions.Response
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &getResponse); err != nil {
		t.Fatalf("failed to decode get response: %v", err)
	}
	if getResponse.Session.FrameCount != 1 || getResponse.Session.EventCount != 1 || getResponse.Session.Status != sessions.StatusActive {
		t.Fatalf("expected active session with counts, got %+v", getResponse.Session)
	}

	finishRecorder := httptest.NewRecorder()
	finishRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+createResponse.Session.ID+"/finish", strings.NewReader(`{"endedUnixMs":1720656123456}`))
	handler.ServeHTTP(finishRecorder, finishRequest)

	if finishRecorder.Code != http.StatusOK {
		t.Fatalf("expected finish status %d, got %d with body %s", http.StatusOK, finishRecorder.Code, finishRecorder.Body.String())
	}
	var finishResponse sessions.Response
	if err := json.Unmarshal(finishRecorder.Body.Bytes(), &finishResponse); err != nil {
		t.Fatalf("failed to decode finish response: %v", err)
	}
	if finishResponse.Session.Status != sessions.StatusFinished || finishResponse.Session.EndedAt == nil || *finishResponse.Session.EndedAt != time.UnixMilli(1720656123456).UTC() || finishResponse.Session.DurationMs == nil || *finishResponse.Session.DurationMs != 123456 {
		t.Fatalf("unexpected finished session: %+v", finishResponse.Session)
	}
}

func TestSessionLifecycleErrors(t *testing.T) {
	handler := routes(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("unknown track", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"source":"flutter","game":"gt7","platform":"ps5","trackId":"missing-track","startedUnixMs":1720656000000}`))
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "trackId must exist in catalog") {
			t.Fatalf("expected catalog validation error, got status %d body %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("missing session", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session_missing", nil)
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), "session_not_found") {
			t.Fatalf("expected session_not_found, got status %d body %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("finish conflict", func(t *testing.T) {
		createRecorder := httptest.NewRecorder()
		createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"source":"flutter","game":"gt7","platform":"ps5","startedUnixMs":1720656000000}`))
		handler.ServeHTTP(createRecorder, createRequest)

		var createResponse sessions.Response
		if err := json.Unmarshal(createRecorder.Body.Bytes(), &createResponse); err != nil {
			t.Fatalf("failed to decode create response: %v", err)
		}

		firstFinish := httptest.NewRecorder()
		handler.ServeHTTP(firstFinish, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+createResponse.Session.ID+"/finish", strings.NewReader(`{}`)))
		if firstFinish.Code != http.StatusOK {
			t.Fatalf("expected first finish OK, got %d body %s", firstFinish.Code, firstFinish.Body.String())
		}

		secondFinish := httptest.NewRecorder()
		handler.ServeHTTP(secondFinish, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+createResponse.Session.ID+"/finish", strings.NewReader(`{}`)))
		if secondFinish.Code != http.StatusConflict || !strings.Contains(secondFinish.Body.String(), "session_finished") {
			t.Fatalf("expected session_finished, got status %d body %s", secondFinish.Code, secondFinish.Body.String())
		}
	})
}

func TestCreateSessionContractRejectsInvalidShape(t *testing.T) {
	handler := routes(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"source":"flutter","game":"gt7","startedUnixMs":1720656000000}`

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(body))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestIngestFramesContractValidatesShape(t *testing.T) {
	handler := newTestHandler(t)
	body := []byte(`{
		"sessionId":"session-1",
		"frames":[{
			"timestampUnixMs":1720656000000,
			"speedMps":58.33,
			"rpm":7100,
			"gear":4,
			"throttle":0.82,
			"brake":0,
			"steering":-0.12,
			"fuelLiters":38.4,
			"positionX":123.4,
			"positionY":5.6,
			"positionZ":789.1,
			"yawRadians":1.57,
			"yawRate":0.03,
			"wheelSpeedFL":58.1,
			"wheelSpeedFR":58.2,
			"wheelSpeedRL":58.4,
			"wheelSpeedRR":58.3,
			"lapNumber":2,
			"currentLapMs":81234,
			"lastLapMs":91345,
			"bestLapMs":90210,
			"isOnTrack":true
		}]
	}`)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", bytes.NewReader(body))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}

	var response struct {
		SessionID          string `json:"sessionId"`
		ReceivedFrames     int    `json:"receivedFrames"`
		AcceptedFrames     int    `json:"acceptedFrames"`
		RejectedFrames     int    `json:"rejectedFrames"`
		AcceptedFromUnixMs int64  `json:"acceptedFromUnixMs"`
		AcceptedToUnixMs   int64  `json:"acceptedToUnixMs"`
		Status             string `json:"status"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.SessionID != "session-1" || response.ReceivedFrames != 1 || response.AcceptedFrames != 1 || response.RejectedFrames != 0 || response.Status != "accepted" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if response.AcceptedFromUnixMs != 1720656000000 || response.AcceptedToUnixMs != 1720656000000 {
		t.Fatalf("unexpected accepted time range: %+v", response)
	}
}

func TestIngestFramesContractRejectsInvalidJSON(t *testing.T) {
	handler := newTestHandler(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(`{"sessionId":"session-1","frames":[`))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "bad_request") || !strings.Contains(recorder.Body.String(), "invalid JSON body") {
		t.Fatalf("expected bad_request invalid JSON envelope, got %s", recorder.Body.String())
	}
}

func TestIngestFramesContractAcceptsPartialWithRejectionSummary(t *testing.T) {
	handler := newTestHandler(t)
	body := `{"sessionId":"session-1","frames":[{"timestampUnixMs":1720656000000,"speedMps":58.33,"rpm":7100,"gear":4,"throttle":1.2,"brake":0,"steering":-0.12,"fuelLiters":38.4,"positionX":123.4,"positionY":5.6,"positionZ":789.1,"lapNumber":2,"currentLapMs":81234,"isOnTrack":true}]}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(body))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}

	var response struct {
		SessionID        string `json:"sessionId"`
		ReceivedFrames   int    `json:"receivedFrames"`
		AcceptedFrames   int    `json:"acceptedFrames"`
		RejectedFrames   int    `json:"rejectedFrames"`
		Status           string `json:"status"`
		RejectionSummary *struct {
			Reasons []struct {
				Code  string `json:"code"`
				Count int    `json:"count"`
			} `json:"reasons"`
		} `json:"rejectionSummary,omitempty"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.SessionID != "session-1" || response.ReceivedFrames != 1 || response.AcceptedFrames != 0 || response.RejectedFrames != 1 || response.Status != "rejected" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if response.RejectionSummary == nil || len(response.RejectionSummary.Reasons) != 1 {
		t.Fatalf("expected rejection summary with 1 reason, got %+v", response.RejectionSummary)
	}
	if response.RejectionSummary.Reasons[0].Code != "invalid_throttle" || response.RejectionSummary.Reasons[0].Count != 1 {
		t.Fatalf("expected invalid_throttle count 1, got %+v", response.RejectionSummary.Reasons[0])
	}
}

func TestIngestFramesPersistsPartialRejectionSummaryForSessionSummary(t *testing.T) {
	handler := newTestHandler(t)
	ingestBody := `{"sessionId":"session-1","frames":[{"timestampUnixMs":1720656000000,"speedMps":57,"rpm":6900,"gear":4,"throttle":0.7,"brake":0,"steering":-0.1,"fuelLiters":38.3,"positionX":122,"positionY":5.5,"positionZ":788,"lapNumber":2,"currentLapMs":81111,"isOnTrack":true},{"timestampUnixMs":1720656000123,"speedMps":58.33,"rpm":7100,"gear":4,"throttle":1.2,"brake":0,"steering":-0.12,"fuelLiters":38.4,"positionX":123.4,"positionY":5.6,"positionZ":789.1,"lapNumber":2,"currentLapMs":81234,"isOnTrack":true}]}`

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(ingestBody)))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected ingest status %d, got %d: %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}

	summaryRecorder := httptest.NewRecorder()
	handler.ServeHTTP(summaryRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/summary", nil))
	if summaryRecorder.Code != http.StatusOK {
		t.Fatalf("expected summary status %d, got %d: %s", http.StatusOK, summaryRecorder.Code, summaryRecorder.Body.String())
	}

	var response struct {
		Session struct {
			RejectedFrames int `json:"rejectedFrames"`
		} `json:"session"`
		RejectionSummary *struct {
			Reasons []struct {
				Code  string `json:"code"`
				Count int    `json:"count"`
			} `json:"reasons"`
		} `json:"rejectionSummary,omitempty"`
	}
	if err := json.Unmarshal(summaryRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if response.Session.RejectedFrames != 1 {
		t.Fatalf("expected session rejectedFrames 1, got %d", response.Session.RejectedFrames)
	}
	if response.RejectionSummary == nil || len(response.RejectionSummary.Reasons) != 1 {
		t.Fatalf("expected detail rejection summary, got %+v", response.RejectionSummary)
	}
	if response.RejectionSummary.Reasons[0].Code != "invalid_throttle" || response.RejectionSummary.Reasons[0].Count != 1 {
		t.Fatalf("unexpected rejection reason: %+v", response.RejectionSummary.Reasons[0])
	}
}

func TestIngestFramesPersistsAllRejectedSummaryForSessionSummary(t *testing.T) {
	handler := newTestHandler(t)
	ingestBody := `{"sessionId":"session-1","frames":[{"timestampUnixMs":1720656000000,"speedMps":58.33,"rpm":7100,"gear":4,"throttle":1.2,"brake":0,"steering":-0.12,"fuelLiters":38.4,"positionX":123.4,"positionY":5.6,"positionZ":789.1,"lapNumber":2,"currentLapMs":81234,"isOnTrack":true}]}`

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(ingestBody)))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected ingest status %d, got %d: %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}

	summaryRecorder := httptest.NewRecorder()
	handler.ServeHTTP(summaryRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/summary", nil))
	if summaryRecorder.Code != http.StatusOK {
		t.Fatalf("expected summary status %d, got %d: %s", http.StatusOK, summaryRecorder.Code, summaryRecorder.Body.String())
	}
	if !strings.Contains(summaryRecorder.Body.String(), `"rejectedFrames":1`) || !strings.Contains(summaryRecorder.Body.String(), `"invalid_throttle"`) {
		t.Fatalf("expected persisted all-rejected diagnostics, got %s", summaryRecorder.Body.String())
	}
}

func TestIngestFramesDoesNotPersistBatchValidationErrors(t *testing.T) {
	handler := newTestHandler(t)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(`{"sessionId":"session-1","frames":[]}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected ingest status %d, got %d: %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}

	summaryRecorder := httptest.NewRecorder()
	handler.ServeHTTP(summaryRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/summary", nil))
	if summaryRecorder.Code != http.StatusOK {
		t.Fatalf("expected summary status %d, got %d: %s", http.StatusOK, summaryRecorder.Code, summaryRecorder.Body.String())
	}
	if strings.Contains(summaryRecorder.Body.String(), "rejectionSummary") {
		t.Fatalf("expected no persisted batch-level diagnostics, got %s", summaryRecorder.Body.String())
	}
}

func TestIngestFramesDiagnosticPersistenceFailureStillReturnsAccepted(t *testing.T) {
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-1")
	frameStore := telemetry.NewFrameStore(100)
	eventStore := events.NewStore(100, events.DedupOptions{})
	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, frameStore, eventStore)
	handler := routesWithAIAndSessions(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		frameStore,
		tracks.OfficialGT7SeedCatalog(),
		eventStore,
		sessionRepo,
		&noopAIService{},
		admin.NewMemoryStatsRepo(sessionRepo, frameStore),
		summaryRepo,
		failingRejectionStore{err: errors.New("diagnostics down")},
	)
	ingestBody := `{"sessionId":"session-1","frames":[{"timestampUnixMs":1720656000000,"speedMps":57,"rpm":6900,"gear":4,"throttle":0.7,"brake":0,"steering":-0.1,"fuelLiters":38.3,"positionX":122,"positionY":5.5,"positionZ":788,"lapNumber":2,"currentLapMs":81111,"isOnTrack":true},{"timestampUnixMs":1720656000123,"speedMps":58.33,"rpm":7100,"gear":4,"throttle":1.2,"brake":0,"steering":-0.12,"fuelLiters":38.4,"positionX":123.4,"positionY":5.6,"positionZ":789.1,"lapNumber":2,"currentLapMs":81234,"isOnTrack":true}]}`

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(ingestBody)))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d despite diagnostic failure, got %d: %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}
}

func TestIngestFramesContractAcceptsOmittedOptionalFields(t *testing.T) {
	handler := newTestHandler(t)
	body := `{"sessionId":"session-1","frames":[{"timestampUnixMs":1720656000000,"speedMps":57,"rpm":6900,"gear":4,"throttle":0.7,"brake":0,"steering":-0.1,"fuelLiters":38.3,"positionX":122,"positionY":5.5,"positionZ":788,"lapNumber":2,"currentLapMs":81111,"isOnTrack":true},{"timestampUnixMs":1720656000123,"speedMps":58.33,"rpm":7100,"gear":4,"throttle":0.82,"brake":0,"steering":-0.12,"fuelLiters":38.4,"positionX":123.4,"positionY":5.6,"positionZ":789.1,"lapNumber":2,"currentLapMs":81234,"isOnTrack":true}]}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(body))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"acceptedFrames":2`) || !strings.Contains(recorder.Body.String(), `"acceptedFromUnixMs":1720656000000`) || !strings.Contains(recorder.Body.String(), `"acceptedToUnixMs":1720656000123`) {
		t.Fatalf("expected accepted count and min/max time range, got %s", recorder.Body.String())
	}
}

func TestIngestFramesContractRejectsBatchesOverMaxSize(t *testing.T) {
	handler := newTestHandler(t)
	var body strings.Builder
	body.WriteString(`{"sessionId":"session-1","frames":[`)
	for i := 0; i < 601; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`{"timestampUnixMs":`)
		body.WriteString(strconv.FormatInt(1720656000000+int64(i), 10))
		body.WriteString(`,"speedMps":58.33,"rpm":7100,"gear":4,"throttle":0.82,"brake":0,"steering":-0.12,"fuelLiters":38.4,"positionX":123.4,"positionY":5.6,"positionZ":789.1,"lapNumber":2,"currentLapMs":81234,"isOnTrack":true}`)
	}
	body.WriteString(`]}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(body.String()))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "frames exceeds maximum batch size") {
		t.Fatalf("expected max batch validation error, got %s", recorder.Body.String())
	}

	var response errorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode error envelope: %v", err)
	}
	if response.Error.Details.RejectionCode != "batch_too_large" || response.Error.Details.Category != "batch" || response.Error.Details.MaxFrames != 600 {
		t.Fatalf("unexpected rejection details: %+v", response.Error.Details)
	}
}

func TestIngestFramesContractRejectsRouteBodySessionMismatch(t *testing.T) {
	handler := newTestHandler(t)
	body := `{"sessionId":"different-session","frames":[{"timestampUnixMs":1720656000000,"throttle":0,"brake":0,"lapNumber":0}]}`

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(body))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	var response errorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode error envelope: %v", err)
	}
	if response.Error.Details.RejectionCode != "session_id_mismatch" || response.Error.Details.Category != "batch" || response.Error.Details.Field != "sessionId" {
		t.Fatalf("unexpected rejection details: %+v", response.Error.Details)
	}
}

func TestIngestFramesContractPartialAcceptsConsistentFrames(t *testing.T) {
	handler := newTestHandler(t)
	body := `{"sessionId":"session-1","frames":[{"timestampUnixMs":1720656000001,"speedMps":58.33,"rpm":7100,"gear":4,"throttle":0.82,"brake":0,"steering":-0.12,"fuelLiters":38.4,"positionX":123.4,"positionY":5.6,"positionZ":789.1,"lapNumber":2,"currentLapMs":81234,"isOnTrack":true},{"timestampUnixMs":1720656000000,"speedMps":58.33,"rpm":7100,"gear":4,"throttle":0.82,"brake":0,"steering":-0.12,"fuelLiters":38.4,"positionX":123.4,"positionY":5.6,"positionZ":789.1,"lapNumber":2,"currentLapMs":81235,"isOnTrack":true}]}`

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(body))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}

	var response struct {
		AcceptedFrames int    `json:"acceptedFrames"`
		RejectedFrames int    `json:"rejectedFrames"`
		Status         string `json:"status"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.AcceptedFrames != 1 || response.RejectedFrames != 1 || response.Status != "partial" {
		t.Fatalf("expected 1 accepted, 1 rejected, status partial; got %+v", response)
	}
}

func TestIngestFramesStoresAcceptedNormalizedFrames(t *testing.T) {
	store := telemetry.NewFrameStore(10)
	handler := newTestHandlerWithStores(t, store, events.NewStore(100, events.DedupOptions{}))
	body := `{"frames":[{"timestampUnixMs":1720656000000,"speedMps":57,"rpm":6900,"gear":4,"throttle":0.7,"brake":0,"steering":-0.1,"fuelLiters":38.3,"positionX":122,"positionY":5.5,"positionZ":788,"lapNumber":2,"currentLapMs":81111,"isOnTrack":true},{"timestampUnixMs":1720656000123,"speedMps":58.33,"rpm":7100,"gear":4,"throttle":0.82,"brake":0,"steering":-0.12,"fuelLiters":38.4,"positionX":123.4,"positionY":5.6,"positionZ":789.1,"lapNumber":2,"currentLapMs":81234,"isOnTrack":true}]}`

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(body))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}
	frames, err := store.Frames(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("get frames: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 retained frames, got %d", len(frames))
	}
	if frames[0].TimestampUnixMs != 1720656000000 || frames[1].TimestampUnixMs != 1720656000123 {
		t.Fatalf("expected accepted frames retained in order, got %+v", frames)
	}
	if frames[0].YawRadians != nil || frames[0].WheelSpeedFL != nil {
		t.Fatalf("expected missing optional fields to remain nil in retention")
	}
}

func TestIngestFramesDoesNotStoreRejectedBatch(t *testing.T) {
	store := telemetry.NewFrameStore(10)
	handler := newTestHandlerWithStores(t, store, events.NewStore(100, events.DedupOptions{}))
	body := `{"sessionId":"session-1","frames":[{"timestampUnixMs":1720656000000,"speedMps":58.33,"rpm":7100,"gear":4,"throttle":1.2,"brake":0,"steering":-0.12,"fuelLiters":38.4,"positionX":123.4,"positionY":5.6,"positionZ":789.1,"lapNumber":2,"currentLapMs":81234,"isOnTrack":true}]}`

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(body))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}
	frames, err := store.Frames(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("get frames: %v", err)
	}
	if got := len(frames); got != 0 {
		t.Fatalf("expected all-rejected batch not to be retained, got %d frames", got)
	}
}

func TestIngestFramesReturnsInternalServerErrorWhenPersistenceFails(t *testing.T) {
	store := failingFrameStore{err: errors.New("append failed")}
	handler := newTestHandlerWithStores(t, store, events.NewStore(100, events.DedupOptions{}))
	body := `{"frames":[{"timestampUnixMs":1720656000000,"speedMps":57,"rpm":6900,"gear":4,"throttle":0.7,"brake":0,"steering":-0.1,"fuelLiters":38.3,"positionX":122,"positionY":5.5,"positionZ":788,"lapNumber":2,"currentLapMs":81111,"isOnTrack":true}]}`

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", strings.NewReader(body))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
}

type failingFrameStore struct {
	err error
}

func (s failingFrameStore) Append(_ context.Context, _ string, _ []telemetry.Frame) error {
	return s.err
}

func (s failingFrameStore) Frames(_ context.Context, _ string) ([]telemetry.Frame, error) {
	return nil, s.err
}

func TestDetectTrackContractReturnsPendingForInsufficientFrames(t *testing.T) {
	store := telemetry.NewFrameStore(10)
	handler := newTestHandlerWithStores(t, store, events.NewStore(100, events.DedupOptions{}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/track", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"status":"pending"`) || !strings.Contains(recorder.Body.String(), `"reason":"insufficient_data"`) || !strings.Contains(recorder.Body.String(), `"nextAction":"collect_more_frames"`) {
		t.Fatalf("expected unknown insufficient frame detection response, got %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"trackId":null`) || !strings.Contains(recorder.Body.String(), `"layoutId":null`) {
		t.Fatalf("expected nullable catalog fields in fallback response, got %s", recorder.Body.String())
	}
}

func TestDetectTrackContractMatchesRetainedFramesAgainstSeedCatalog(t *testing.T) {
	store := telemetry.NewFrameStore(40)
	if err := store.Append(context.Background(), "session-1", apiStraightCompletedLapFrames(5423, 32)); err != nil {
		t.Fatalf("append frames: %v", err)
	}
	handler := newTestHandlerWithStores(t, store, events.NewStore(100, events.DedupOptions{}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/track", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response tracks.DetectionResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Status != tracks.DetectionStatusDetected || response.TrackID == nil || *response.TrackID != "gt7_watkins_glen_international" || response.LayoutID == nil || *response.LayoutID != "gt7_layout_1240" {
		t.Fatalf("unexpected detection response: %+v", response)
	}
	if len(response.Reasons) != 1 || response.Reasons[0] != tracks.DetectionReasonLengthMatch || response.NextAction != tracks.DetectionNextActionUseDetectedLayout {
		t.Fatalf("expected stable reason and next action, got %+v", response)
	}
}

func TestListEventsContractReturnsStoredEvents(t *testing.T) {
	store := events.NewStore(10, events.DedupOptions{})
	seedAPIEvent(t, store, apiEngineerEvent(func(event *events.EngineerEvent) {
		event.EventID = "event-api-1"
	}))
	handler := newTestHandlerWithStores(t, telemetry.NewFrameStore(10), store)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/events", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"sessionId":"session-1"`) || !strings.Contains(recorder.Body.String(), `"eventId":"event-api-1"`) || !strings.Contains(recorder.Body.String(), `"version":"telemetry-one.engineer-event.v1"`) {
		t.Fatalf("expected stored event JSON shape, got %s", recorder.Body.String())
	}
	for _, forbidden := range []string{`"positionX":`, `"speedMps":`, `"throttle":`, `"brake":`} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("expected API event response not to leak raw telemetry field %s: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestListEventsContractReturnsEmptyResult(t *testing.T) {
	store := events.NewStore(10, events.DedupOptions{})
	handler := newTestHandlerWithStores(t, telemetry.NewFrameStore(10), store)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/events", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"sessionId":"session-1"`) || !strings.Contains(recorder.Body.String(), `"events":[]`) {
		t.Fatalf("expected empty events response, got %s", recorder.Body.String())
	}
}

func TestListEventsContractFiltersByLapCornerAndType(t *testing.T) {
	store := events.NewStore(10, events.DedupOptions{})
	seedAPIEvent(t, store, apiEngineerEvent(func(event *events.EngineerEvent) {
		event.EventID = "event-match"
		event.LapNumber = 3
		event.Type = events.TypeLowExitSpeed
		event.Source.RuleID = "low_exit_speed.v1"
		event.Corner = &events.CatalogRef{ID: apiStringPtr("corner-filter"), DisplayStrategy: events.DisplayStrategyIDOnly}
	}))
	seedAPIEvent(t, store, apiEngineerEvent(func(event *events.EngineerEvent) {
		event.EventID = "event-other"
		event.TimestampUnixMs += 1
		event.LapNumber = 4
		event.Corner = &events.CatalogRef{ID: apiStringPtr("corner-other"), DisplayStrategy: events.DisplayStrategyIDOnly}
	}))
	handler := newTestHandlerWithStores(t, telemetry.NewFrameStore(10), store)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/events?lapNumber=3&cornerId=corner-filter&type=low_exit_speed", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"eventId":"event-match"`) || strings.Contains(recorder.Body.String(), `"eventId":"event-other"`) {
		t.Fatalf("expected filtered event response, got %s", recorder.Body.String())
	}
}

func TestListEventsContractDoesNotInventCatalogNames(t *testing.T) {
	store := events.NewStore(10, events.DedupOptions{})
	seedAPIEvent(t, store, apiEngineerEvent(func(event *events.EngineerEvent) {
		event.EventID = "event-id-only-corner"
		event.Corner = &events.CatalogRef{ID: apiStringPtr("corner-id-only"), DisplayStrategy: events.DisplayStrategyIDOnly}
	}))
	handler := newTestHandlerWithStores(t, telemetry.NewFrameStore(10), store)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/events", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "Turn 1") || !strings.Contains(recorder.Body.String(), `"corner":{"id":"corner-id-only","name":null,"displayStrategy":"id_only"}`) {
		t.Fatalf("expected ID-only corner without invented name, got %s", recorder.Body.String())
	}
}

func TestListEventsContractRejectsInvalidFilters(t *testing.T) {
	store := events.NewStore(10, events.DedupOptions{})
	handler := newTestHandlerWithStores(t, telemetry.NewFrameStore(10), store)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-1/events?lapNumber=-1&type=weak_corner", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func apiStraightCompletedLapFrames(lengthMeters float64, count int) []telemetry.Frame {
	frames := make([]telemetry.Frame, 0, count)
	lastIndex := count - 1
	for i := 0; i < count; i++ {
		lapNumber := 1
		if i == lastIndex {
			lapNumber = 2
		}
		frames = append(frames, telemetry.Frame{
			TimestampUnixMs: int64(i + 1),
			PositionX:       lengthMeters * float64(i) / float64(lastIndex),
			LapNumber:       lapNumber,
			IsOnTrack:       true,
		})
	}

	return frames
}

func seedAPIEvent(t *testing.T, store *events.Store, event events.EngineerEvent) {
	t.Helper()
	if _, accepted, err := store.Append(context.Background(), event); err != nil || !accepted {
		t.Fatalf("expected seed event accepted, accepted=%v err=%v", accepted, err)
	}
}

func apiEngineerEvent(mutate func(*events.EngineerEvent)) events.EngineerEvent {
	event := events.EngineerEvent{
		EventID:         "event-api",
		SessionID:       "session-1",
		Version:         events.ContractVersionV1,
		Type:            events.TypeLateThrottle,
		Severity:        events.SeverityMedium,
		Confidence:      0.82,
		TimestampUnixMs: 1720656012345,
		TimeRange:       &events.TimeRange{StartUnixMs: 1720656012000, EndUnixMs: 1720656012345},
		LapNumber:       2,
		Track:           &events.CatalogRef{ID: apiStringPtr("track-1"), Name: apiStringPtr("Catalog Track"), DisplayStrategy: events.DisplayStrategyCatalogName},
		Layout:          &events.CatalogRef{ID: apiStringPtr("layout-1"), Name: apiStringPtr("Catalog Layout"), DisplayStrategy: events.DisplayStrategyCatalogName},
		Corner:          &events.CatalogRef{ID: apiStringPtr("corner-1"), Name: apiStringPtr("Catalog Corner"), DisplayStrategy: events.DisplayStrategyCatalogName},
		Metrics: []events.MetricEvidence{
			{Name: "throttleReapplicationDeltaMs", Value: 320, Unit: "ms", Status: events.MetricStatusAvailable, Role: events.MetricRoleDelta},
		},
		Source: events.EventSource{Kind: events.SourceDeterministicRule, RuleID: "late_throttle.v1", RuleVersion: "v1"},
	}
	if mutate != nil {
		mutate(&event)
	}

	return event
}

func apiStringPtr(value string) *string {
	return &value
}

func decodeJSONMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("failed to decode JSON object: %v", err)
	}
	return response
}

func assertAPIVersion(t *testing.T, response map[string]any) {
	t.Helper()
	if got, ok := response["apiVersion"].(string); !ok || got != responseAPIVersion {
		t.Fatalf("expected apiVersion %q, got %#v in %+v", responseAPIVersion, response["apiVersion"], response)
	}
}

type failingEventStore struct{}

func (failingEventStore) Append(context.Context, events.EngineerEvent) (events.EngineerEvent, bool, error) {
	return events.EngineerEvent{}, false, errors.New("event store unavailable")
}

func (failingEventStore) List(context.Context, events.Query) ([]events.EngineerEvent, error) {
	return nil, errors.New("event store unavailable")
}

func seedTestSession(t *testing.T, repo sessions.Repository, id string) sessions.Session {
	t.Helper()
	s, err := repo.Create(context.Background(), sessions.Session{
		ID: id, Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: time.UnixMilli(1720656000000).UTC(),
	})
	if err != nil {
		t.Fatalf("seed session %s: %v", id, err)
	}
	return s
}

func TestIngestFramesAttachesTrackLayoutToGeneratedEvents(t *testing.T) {
	store := telemetry.NewFrameStore(64)
	eventStore := events.NewStore(100, events.DedupOptions{})
	catalog := tracks.OfficialGT7SeedCatalog()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-1")
	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store, catalog, eventStore, sessionRepo,
	)

	const lengthMeters = 5423.0
	const count = 34
	body := ingestFrameBatchBody(lengthMeters, count)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-1/frames", bytes.NewReader(body))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}

	storedEvents, err := eventStore.List(context.Background(), events.Query{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(storedEvents) == 0 {
		t.Fatalf("expected at least one generated event after ingest")
	}

	var seenTrack, seenLayout bool
	for _, e := range storedEvents {
		if e.Track != nil && *e.Track.ID == "gt7_watkins_glen_international" {
			seenTrack = true
		}
		if e.Layout != nil && *e.Layout.ID == "gt7_layout_1240" {
			seenLayout = true
		}
		if e.Corner != nil {
			t.Fatalf("expected Corner to remain nil on frame events, got %+v", e.Corner)
		}
	}
	if !seenTrack {
		t.Fatalf("expected generated events to have Watkins Glen track ref, stored events: %+v", storedEvents)
	}
	if !seenLayout {
		t.Fatalf("expected generated events to have Watkins Glen Long Course layout ref, stored events: %+v", storedEvents)
	}
}

func TestIngestFramesDoesNotAttachRefsBeforeLapOneBegins(t *testing.T) {
	store := telemetry.NewFrameStore(64)
	eventStore := events.NewStore(100, events.DedupOptions{})
	catalog := tracks.OfficialGT7SeedCatalog()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-prerace")
	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store, catalog, eventStore, sessionRepo,
	)

	const lengthMeters = 5423.0
	const count = 34
	frames := make([]telemetry.Frame, count)
	lastIndex := count - 1
	for i := 0; i < count; i++ {
		frames[i] = telemetry.Frame{
			TimestampUnixMs: int64(i + 1),
			PositionX:       lengthMeters * float64(i) / float64(lastIndex),
			PositionY:       5.6,
			PositionZ:       789.1,
			LapNumber:       0,
			IsOnTrack:       true,
			SpeedMps:        0,
			RPM:             800,
			Throttle:        0,
			Brake:           0,
			Steering:        0,
			FuelLiters:      40.0,
			CurrentLapMs:    0,
		}
	}
	body, _ := json.Marshal(telemetry.IngestBatchRequest{
		Frames: frames,
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-prerace/frames", bytes.NewReader(body))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}

	storedEvents, err := eventStore.List(context.Background(), events.Query{SessionID: "session-prerace"})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	for _, e := range storedEvents {
		if e.Track != nil {
			t.Fatalf("expected nil track before lap 1 begins, got %+v", e.Track)
		}
		if e.Layout != nil {
			t.Fatalf("expected nil layout before lap 1 begins, got %+v", e.Layout)
		}
	}
}

func TestIngestFramesPersistsDetectedTrackLayoutOnSession(t *testing.T) {
	store := telemetry.NewFrameStore(64)
	eventStore := events.NewStore(100, events.DedupOptions{})
	catalog := tracks.OfficialGT7SeedCatalog()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-detect-persist")
	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store, catalog, eventStore, sessionRepo,
	)

	const lengthMeters = 5423.0
	const count = 34
	body := ingestFrameBatchBody(lengthMeters, count)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-detect-persist/frames", bytes.NewReader(body))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}

	session, err := sessionRepo.FindByID(context.Background(), "session-detect-persist")
	if err != nil {
		t.Fatalf("find session: %v", err)
	}
	if session.DetectedTrackID != "gt7_watkins_glen_international" {
		t.Fatalf("expected detectedTrackId gt7_watkins_glen_international, got %q", session.DetectedTrackID)
	}
	if session.DetectedLayoutID != "gt7_layout_1240" {
		t.Fatalf("expected detectedLayoutId gt7_layout_1240, got %q", session.DetectedLayoutID)
	}

	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, store, eventStore)
	summary, err := summaryRepo.Summary(context.Background(), "session-detect-persist")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.Session.DetectedTrackID == nil || *summary.Session.DetectedTrackID != "gt7_watkins_glen_international" {
		t.Fatalf("expected detectedTrackId in summary, got %+v", summary.Session.DetectedTrackID)
	}
	if summary.Session.DetectedLayoutID == nil || *summary.Session.DetectedLayoutID != "gt7_layout_1240" {
		t.Fatalf("expected detectedLayoutId in summary, got %+v", summary.Session.DetectedLayoutID)
	}

	listResp, err := summaryRepo.List(context.Background(), sessions.SummaryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, item := range listResp.Sessions {
		if item.ID == "session-detect-persist" {
			found = true
			if item.DetectedTrackID == nil || *item.DetectedTrackID != "gt7_watkins_glen_international" {
				t.Fatalf("expected detectedTrackId in list, got %+v", item.DetectedTrackID)
			}
			if item.DetectedLayoutID == nil || *item.DetectedLayoutID != "gt7_layout_1240" {
				t.Fatalf("expected detectedLayoutId in list, got %+v", item.DetectedLayoutID)
			}
		}
	}
	if !found {
		t.Fatal("expected session-detect-persist in list response")
	}
}

func TestIngestFramesDoesNotPersistDetectedBeforeLapOne(t *testing.T) {
	store := telemetry.NewFrameStore(64)
	eventStore := events.NewStore(100, events.DedupOptions{})
	catalog := tracks.OfficialGT7SeedCatalog()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-prerace-nopersist")
	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store, catalog, eventStore, sessionRepo,
	)

	const count = 10
	frames := make([]telemetry.Frame, count)
	for i := 0; i < count; i++ {
		frames[i] = telemetry.Frame{
			TimestampUnixMs: int64(i + 1),
			LapNumber:       0,
			IsOnTrack:       true,
			PositionX:       float64(i) * 100,
			PositionY:       5.6,
			PositionZ:       789.1,
			SpeedMps:        58.33,
			RPM:             7100,
			Gear:            4,
			Throttle:        0.7,
			Brake:           0,
			Steering:        -0.1,
			FuelLiters:      38.4,
		}
	}
	body, _ := json.Marshal(telemetry.IngestBatchRequest{Frames: frames})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-prerace-nopersist/frames", bytes.NewReader(body))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}

	session, err := sessionRepo.FindByID(context.Background(), "session-prerace-nopersist")
	if err != nil {
		t.Fatalf("find session: %v", err)
	}
	if session.DetectedTrackID != "" {
		t.Fatalf("expected empty detectedTrackId before lap 1, got %q", session.DetectedTrackID)
	}
	if session.DetectedLayoutID != "" {
		t.Fatalf("expected empty detectedLayoutId before lap 1, got %q", session.DetectedLayoutID)
	}
}

func TestIngestFramesDoesNotOverrideManualTrackID(t *testing.T) {
	store := telemetry.NewFrameStore(64)
	eventStore := events.NewStore(100, events.DedupOptions{})
	catalog := tracks.OfficialGT7SeedCatalog()
	sessionRepo := sessions.NewMemoryRepository()
	ctx := context.Background()
	startedAt := time.UnixMilli(1720656000000).UTC()
	sessionRepo.Create(ctx, sessions.Session{
		ID: "session-manual-track", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: startedAt,
		TrackID: "gt7_watkins_glen_international",
	})
	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store, catalog, eventStore, sessionRepo,
	)

	const lengthMeters = 5423.0
	const count = 34
	body := ingestFrameBatchBody(lengthMeters, count)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-manual-track/frames", bytes.NewReader(body))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}

	session, err := sessionRepo.FindByID(ctx, "session-manual-track")
	if err != nil {
		t.Fatalf("find session: %v", err)
	}
	if session.DetectedTrackID != "" {
		t.Fatalf("expected detectedTrackId to remain empty when manual TrackID is set, got %q", session.DetectedTrackID)
	}
	if session.DetectedLayoutID != "" {
		t.Fatalf("expected detectedLayoutId to remain empty when manual TrackID is set, got %q", session.DetectedLayoutID)
	}
	if session.TrackID != "gt7_watkins_glen_international" {
		t.Fatalf("expected manual TrackID to be preserved, got %q", session.TrackID)
	}
}

func TestIngestFramesDoesNotAttachRefsForPendingDetection(t *testing.T) {
	store := telemetry.NewFrameStore(64)
	eventStore := events.NewStore(100, events.DedupOptions{})
	catalog := tracks.OfficialGT7SeedCatalog()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-pending")
	handler := routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store, catalog, eventStore, sessionRepo,
	)

	frames := make([]telemetry.Frame, 5)
	for i := 0; i < 4; i++ {
		frames[i] = telemetry.Frame{
			TimestampUnixMs: int64(i+1) * 1000,
			LapNumber:       1,
			CurrentLapMs:    int64(i+1) * 1000,
			IsOnTrack:       true,
			PositionX:       float64(i) * 100,
			PositionY:       5.6,
			PositionZ:       789.1,
			SpeedMps:        58.33,
			RPM:             7100,
			Gear:            4,
			Throttle:        0.7,
			Brake:           0,
			Steering:        -0.1,
			FuelLiters:      38.4,
		}
	}
	lastLapMs := int64(95000)
	bestLapMs := int64(90000)
	frames[4] = telemetry.Frame{
		TimestampUnixMs: 5000,
		LapNumber:       2,
		CurrentLapMs:    100,
		LastLapMs:       &lastLapMs,
		BestLapMs:       &bestLapMs,
		IsOnTrack:       true,
		PositionX:       400,
		PositionY:       5.6,
		PositionZ:       789.1,
		SpeedMps:        58.33,
		RPM:             7100,
		Gear:            4,
		Throttle:        0.7,
		Brake:           0,
		Steering:        -0.1,
		FuelLiters:      38.4,
	}
	body, err := json.Marshal(telemetry.IngestBatchRequest{
		SessionID: "session-pending",
		Frames:    frames,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-pending/frames", bytes.NewReader(body))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
	}

	storedEvents, err := eventStore.List(context.Background(), events.Query{SessionID: "session-pending"})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(storedEvents) == 0 {
		t.Fatalf("expected at least one generated event while detection is pending, got none")
	}
	for _, e := range storedEvents {
		if e.Track != nil {
			t.Fatalf("expected nil track for pending detection, got %+v", e.Track)
		}
		if e.Layout != nil {
			t.Fatalf("expected nil layout for pending detection, got %+v", e.Layout)
		}
	}
}

func ingestFrameBatchBody(lengthMeters float64, count int) []byte {
	frames := make([]telemetry.Frame, count)
	lastIndex := count - 1
	for i := 0; i < count; i++ {
		lapNumber := 1
		if i == lastIndex {
			lapNumber = 2
		}
		frames[i] = telemetry.Frame{
			TimestampUnixMs: int64(i + 1),
			PositionX:       lengthMeters * float64(i) / float64(lastIndex),
			LapNumber:       lapNumber,
			IsOnTrack:       true,
			SpeedMps:        58.33,
			RPM:             7100,
			Gear:            4,
			Throttle:        0.7,
			Brake:           0,
			Steering:        -0.1,
			FuelLiters:      38.4,
			PositionY:       5.6,
			PositionZ:       789.1,
			CurrentLapMs:    81234,
		}
		if i == lastIndex {
			bestLapMs := int64(84000)
			frames[i].LastLapMs = &bestLapMs
			frames[i].BestLapMs = &bestLapMs
		} else {
			lastLapMs := int64(92000)
			bestLapMs := int64(84000)
			frames[i].LastLapMs = &lastLapMs
			frames[i].BestLapMs = &bestLapMs
		}
	}
	body, _ := json.Marshal(telemetry.IngestBatchRequest{
		Frames: frames,
	})
	return body
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-1")
	return routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		telemetry.NewFrameStore(100),
		tracks.OfficialGT7SeedCatalog(),
		events.NewStore(100, events.DedupOptions{}),
		sessionRepo,
	)
}

func newTestHandlerWithStores(t *testing.T, frameStore telemetry.Store, eventStore events.Repository) http.Handler {
	t.Helper()
	sessionRepo := sessions.NewMemoryRepository()
	seedTestSession(t, sessionRepo, "session-1")
	return routesWithSessionRepository(
		config.Config{Addr: ":0", Env: "test"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		frameStore,
		tracks.OfficialGT7SeedCatalog(),
		eventStore,
		sessionRepo,
	)
}

type failingRejectionStore struct {
	err error
}

type failingLapRepository struct {
	err error
}

func (r failingLapRepository) UpsertCompleted(context.Context, []laps.CompletedLap) error {
	return r.err
}

func (r failingLapRepository) UpdateTelemetryGapCount(context.Context, string, int, int) error {
	return r.err
}

func (r failingLapRepository) ListBySession(context.Context, string) ([]laps.CompletedLap, error) {
	return nil, r.err
}

func (s failingRejectionStore) Append(context.Context, telemetry.RejectionSummaryRecord) error {
	return s.err
}

func (s failingRejectionStore) Summary(context.Context, string) (telemetry.RejectedSummaryAggregate, error) {
	return telemetry.RejectedSummaryAggregate{}, nil
}

func (s failingRejectionStore) Summaries(context.Context, []string) (map[string]telemetry.RejectedSummaryAggregate, error) {
	return map[string]telemetry.RejectedSummaryAggregate{}, nil
}
