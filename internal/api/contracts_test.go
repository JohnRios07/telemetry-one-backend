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

	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/events"
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
